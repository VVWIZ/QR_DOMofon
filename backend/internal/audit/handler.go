package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// MCResolver извлекает management_company_id текущего запроса из claims. Передаётся
// адаптером cmd/server (auth.MCIDFromContext) — прямой импорт auth сюда создал бы
// цикл (auth → audit.Recorder), поэтому связь инвертирована через функцию.
type MCResolver func(ctx context.Context) string

// Handler обслуживает GET /api/v1/audit/events?limit=N (скоуп по mc_id из claims).
type Handler struct {
	recorder *PgRecorder
	mcID     MCResolver
}

// NewHandler создаёт HTTP-хендлер аудита с резолвером mc_id (скоуп admin).
func NewHandler(recorder *PgRecorder, mcID MCResolver) *Handler {
	return &Handler{recorder: recorder, mcID: mcID}
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

	rows, err := h.recorder.List(r.Context(), h.mcID(r.Context()), limit)
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
