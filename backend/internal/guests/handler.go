package guests

// HTTP-хендлеры гостевого доступа. Создание/управление — за authn+RequireResident
// (проверки права — в сервисе, по БД). Страница и открытие гостя — публичные по
// токену ссылки (bearer-capability); ответы /g/* помечаются no-store/no-referrer,
// токен в них и в логи не попадает.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает эндпоинты гостей.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер гостей.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- Тела запросов ---

type createGuestRequest struct {
	FullName             string   `json:"full_name"`
	ValidHours           float64  `json:"valid_hours"` // окно от now; ≤ 48
	AccessPointPublicIDs []string `json:"access_point_public_ids"`
}

type openGuestRequest struct {
	PublicID string `json:"public_id"`
}

type permissionRequest struct {
	Enabled bool `json:"enabled"`
}

// --- Хендлеры создателя (resident) ---

// GuestPoints — GET /apartments/{apartment_id}/guest-points.
func (h *Handler) GuestPoints(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	opts, apiErr := h.svc.GuestPointOptions(r.Context(), claims, chi.URLParam(r, "apartment_id"))
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	points := make([]map[string]any, 0, len(opts))
	for _, o := range opts {
		points = append(points, map[string]any{"public_id": o.PublicID, "label": o.Label, "type": o.Type})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"points": points})
}

// CreateGuest — POST /apartments/{apartment_id}/guests.
func (h *Handler) CreateGuest(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	var req createGuestRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.FullName == "" || req.ValidHours <= 0 {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields full_name and valid_hours are required", rid)
		return
	}
	validTo := h.svc.now().Add(time.Duration(req.ValidHours * float64(time.Hour)))
	res, apiErr := h.svc.CreateGuest(r.Context(), claims, chi.URLParam(r, "apartment_id"), req.FullName, validTo, req.AccessPointPublicIDs)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"guest_id":   res.GuestID,
		"token":      res.Token,
		"url":        res.URL,
		"valid_from": res.ValidFrom.UTC().Format(time.RFC3339),
		"valid_to":   res.ValidTo.UTC().Format(time.RFC3339),
	})
}

// ListGuests — GET /apartments/{apartment_id}/guests (свои гости создателя).
func (h *Handler) ListGuests(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	rows, apiErr := h.svc.ListGuests(r.Context(), claims)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, g := range rows {
		out = append(out, map[string]any{
			"id":         g.ID,
			"full_name":  g.FullName,
			"valid_from": g.ValidFrom.UTC().Format(time.RFC3339),
			"valid_to":   g.ValidTo.UTC().Format(time.RFC3339),
			"revoked":    g.RevokedAt != nil,
			"points":     g.Points,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"guests": out})
}

// RevokeGuest — POST /guests/{guest_id}/revoke.
func (h *Handler) RevokeGuest(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	if apiErr := h.svc.Revoke(r.Context(), claims, chi.URLParam(r, "guest_id")); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// SetPermission — PUT /apartments/{apartment_id}/residents/{user_id}/guest-permission.
func (h *Handler) SetPermission(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	var req permissionRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	apiErr := h.svc.SetPermission(r.Context(), claims, chi.URLParam(r, "apartment_id"), chi.URLParam(r, "user_id"), req.Enabled)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// --- Публичные хендлеры гостя (по токену ссылки) ---

// View — GET /g/{token}: данные гостевой страницы.
func (h *Handler) View(w http.ResponseWriter, r *http.Request) {
	guardHeaders(w)
	view, apiErr := h.svc.ViewGuest(r.Context(), chi.URLParam(r, "token"))
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	points := make([]map[string]any, 0, len(view.Points))
	for _, p := range view.Points {
		points = append(points, map[string]any{"public_id": p.PublicID, "label": p.Label, "type": p.Type, "online": p.Online})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"guest_name": view.FullName,
		"valid_from": view.ValidFrom.UTC().Format(time.RFC3339),
		"valid_to":   view.ValidTo.UTC().Format(time.RFC3339),
		"points":     points,
	})
}

// Open — POST /g/{token}/open: открытие точки гостем.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	guardHeaders(w)
	rid := httpx.RequestIDFromContext(r.Context())
	var req openGuestRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.PublicID == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field public_id is required", rid)
		return
	}
	res, apiErr := h.svc.OpenAsGuest(r.Context(), chi.URLParam(r, "token"), req.PublicID)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"request_id": res.RequestID, "status": res.Status})
}

// --- Вспомогательное ---

// guardHeaders помечает ответы гостевых эндпоинтов: не кэшировать, не утекать
// токен из URL в Referer.
func guardHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func (h *Handler) claims(w http.ResponseWriter, r *http.Request) (auth.Claims, bool) {
	c, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return auth.Claims{}, false
	}
	return c, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any, rid string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return false
	}
	return true
}
