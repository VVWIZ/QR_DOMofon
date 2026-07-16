package schedules

// HTTP-хендлеры расписаний (УК-админ). Скоуп/проверки — в сервисе.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает /api/v1/admin/schedule-* и .../schedules.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type createScheduleRequest struct {
	Dow      int    `json:"dow"`
	Opens    string `json:"opens"`
	Closes   string `json:"closes"`
	Timezone string `json:"timezone"`
}

// ListPoints — GET /api/v1/admin/schedule-points.
func (h *Handler) ListPoints(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	pts, apiErr := h.svc.ListPoints(r.Context(), claims)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	out := make([]map[string]any, 0, len(pts))
	for _, p := range pts {
		sch := make([]map[string]any, 0, len(p.Schedules))
		for _, s := range p.Schedules {
			sch = append(sch, map[string]any{"id": s.ID, "dow": s.Dow, "opens": s.Opens, "closes": s.Closes, "timezone": s.Timezone, "is_active": s.IsActive})
		}
		out = append(out, map[string]any{"public_id": p.PublicID, "label": p.Label, "type": p.Type, "schedules": sch})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"points": out})
}

// Create — POST /api/v1/admin/access-points/{public_id}/schedules.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	var req createScheduleRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return
	}
	id, apiErr := h.svc.Create(r.Context(), claims, chi.URLParam(r, "public_id"), Schedule{Dow: req.Dow, Opens: req.Opens, Closes: req.Closes, Timezone: req.Timezone})
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// Delete — DELETE /api/v1/admin/schedules/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	if apiErr := h.svc.Delete(r.Context(), claims, chi.URLParam(r, "id")); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) claims(w http.ResponseWriter, r *http.Request) (auth.Claims, bool) {
	c, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return auth.Claims{}, false
	}
	return c, true
}
