package audit

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает GET /api/v1/audit/events?limit=N.
type Handler struct {
	recorder *PgRecorder
}

// NewHandler создаёт HTTP-хендлер аудита.
func NewHandler(recorder *PgRecorder) *Handler {
	return &Handler{recorder: recorder}
}

// eventJSON — форма события в ответе (api.md GET /audit/events). Nullable-поля —
// указатели: отсутствие → JSON null.
type eventJSON struct {
	ID                  int64           `json:"id"`
	EventType           string          `json:"event_type"`
	OccurredAt          string          `json:"occurred_at"`
	Actor               *string         `json:"actor"`
	ApartmentID         *string         `json:"apartment_id"`
	AccessPointID       *string         `json:"access_point_id"`
	DeviceID            *string         `json:"device_id"`
	CallID              *string         `json:"call_id"`
	RequestID           *string         `json:"request_id"`
	ManagementCompanyID *string         `json:"management_company_id"`
	Metadata            json.RawMessage `json:"metadata,omitempty"`
}

// List отдаёт последние события аудита (новые первыми).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	log := httpx.LoggerFromContext(r.Context())

	limit := 0
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}

	rows, err := h.recorder.List(r.Context(), limit)
	if err != nil {
		log.Error("audit_list_failed", "error", err)
		httpx.WriteError(w, httpx.CodeInternal, "Failed to read audit events", httpx.RequestIDFromContext(r.Context()))
		return
	}

	events := make([]eventJSON, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventJSON{
			ID:                  row.ID,
			EventType:           row.EventType,
			OccurredAt:          row.OccurredAt.UTC().Format(time.RFC3339),
			Actor:               row.Actor,
			ApartmentID:         row.ApartmentID,
			AccessPointID:       row.AccessPointID,
			DeviceID:            row.DeviceID,
			CallID:              row.CallID,
			RequestID:           row.RequestID,
			ManagementCompanyID: row.ManagementCompanyID,
			Metadata:            row.Metadata,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}
