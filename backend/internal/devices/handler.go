package devices

import (
	"net/http"
	"time"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает GET /api/v1/devices (список с derived-статусом).
type Handler struct {
	repo     *Repo
	presence *Presence
}

// NewHandler создаёт хендлер реестра устройств.
func NewHandler(repo *Repo, presence *Presence) *Handler {
	return &Handler{repo: repo, presence: presence}
}

// deviceJSON — форма устройства в ответе (api.md GET /devices).
type deviceJSON struct {
	ID              string  `json:"id"`
	Serial          string  `json:"serial"`
	AccessPointID   string  `json:"access_point_id"`
	Type            string  `json:"type"`
	FirmwareVersion string  `json:"firmware_version"`
	Status          string  `json:"status"`
	LastSeenAt      *string `json:"last_seen_at"`
}

// List отдаёт устройства; status — производное от Redis-presence.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := httpx.LoggerFromContext(ctx)

	// Скоуп admin: список ограничен УК из claims (auth.md §5).
	claims, _ := auth.ClaimsFromContext(ctx)
	devices, err := h.repo.List(ctx, claims.MCID)
	if err != nil {
		log.Error("devices_list_failed", "error", err)
		httpx.WriteError(w, httpx.CodeInternal, "Failed to list devices", httpx.RequestIDFromContext(ctx))
		return
	}

	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		online, err := h.presence.IsOnline(ctx, d.ID)
		if err != nil {
			log.Error("presence_check_failed", "device_id", d.ID, "error", err)
			online = false
		}
		status := "offline"
		if online {
			status = "online"
		}

		var lastSeen *string
		if d.LastSeenAt != nil {
			s := d.LastSeenAt.UTC().Format(time.RFC3339)
			lastSeen = &s
		}

		out = append(out, deviceJSON{
			ID:              d.ID,
			Serial:          d.Serial,
			AccessPointID:   d.AccessPointID,
			Type:            d.Type,
			FirmwareVersion: d.FirmwareVersion,
			Status:          status,
			LastSeenAt:      lastSeen,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": out})
}
