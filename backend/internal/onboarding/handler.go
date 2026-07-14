package onboarding

// HTTP-хендлеры онбординга (api.md). Приём инвайта — публичный (вход без OTP,
// возвращает пару токенов как логин); остальное — за authn+RBAC (admin/owner),
// проверки скоупа — в сервисе.

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает эндпоинты онбординга.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер онбординга.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- Тела запросов ---

type acceptRequest struct {
	Token string `json:"token"`
}

type ownerRequest struct {
	ApartmentID string `json:"apartment_id"`
	Phone       string `json:"phone"`
}

type grantRequest struct {
	AccessPointPublicID string `json:"access_point_public_id"`
	Phone               string `json:"phone"`
}

type residentInviteRequest struct {
	Phone string `json:"phone"`
}

// --- Тела ответов ---

type inviteJSON struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type apartmentJSON struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Role   string `json:"role"`
}

type grantJSON struct {
	PublicID string `json:"public_id"`
	Label    string `json:"label"`
}

type residentJSON struct {
	UserID     string          `json:"user_id"`
	Phone      string          `json:"phone"`
	Kind       string          `json:"kind"`
	Apartments []apartmentJSON `json:"apartments"`
	Grants     []grantJSON     `json:"grants"`
}

// AcceptInvite — POST /api/v1/auth/invite/accept (публичный).
//
// НОВЫЙ пользователь → сразу вход без OTP: ответ как у otp/verify (access-токен
// + refresh-cookie). СУЩЕСТВУЮЩИЙ пользователь → привязка/грант созданы, но
// сессия по ссылке НЕ выдаётся (иначе — угон аккаунта по известному телефону):
// 200 {linked:true, login_required:true}, вход обычным OTP.
func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	var req acceptRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.Token == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field token is required", rid)
		return
	}
	res, apiErr := h.svc.AcceptInvite(r.Context(), req.Token)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	if res.Created {
		auth.WriteLogin(w, res.Login)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"linked":         true,
		"login_required": true,
	})
}

// CreateOwner — POST /api/v1/admin/owners (УК-админ): инвайт владельца на квартиру.
func (h *Handler) CreateOwner(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	var req ownerRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.ApartmentID == "" || req.Phone == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields apartment_id and phone are required", rid)
		return
	}
	res, apiErr := h.svc.CreateOwnerInvite(r.Context(), claims, req.ApartmentID, req.Phone)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"invite": inviteBody(res)})
}

// CreateAccessGrant — POST /api/v1/admin/access-grants (УК-админ): доступ на
// калитку/шлагбаум. 200 — грант выдан сразу; 201 — выпущен инвайт.
func (h *Handler) CreateAccessGrant(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	var req grantRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.AccessPointPublicID == "" || req.Phone == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields access_point_public_id and phone are required", rid)
		return
	}
	res, apiErr := h.svc.CreateAccessGrant(r.Context(), claims, req.AccessPointPublicID, req.Phone)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	if res.Granted {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"granted":                true,
			"user_id":                res.UserID,
			"access_point_public_id": res.AccessPointPublicID,
		})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"granted": false,
		"invite":  inviteBody(*res.Invite),
	})
}

// ListResidents — GET /api/v1/admin/residents (УК-админ): жильцы/владельцы своей УК.
func (h *Handler) ListResidents(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	res, apiErr := h.svc.ListResidents(r.Context(), claims)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"residents": residentsBody(res)})
}

// InviteResident — POST /api/v1/apartments/{apartment_id}/residents/invite
// (владелец): инвайт жильца в свою квартиру.
func (h *Handler) InviteResident(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	apartmentID := chi.URLParam(r, "apartment_id")
	if apartmentID == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "apartment_id is required", rid)
		return
	}
	var req residentInviteRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.Phone == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field phone is required", rid)
		return
	}
	res, apiErr := h.svc.CreateResidentInvite(r.Context(), claims, apartmentID, req.Phone)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"invite": inviteBody(res)})
}

// --- Вспомогательное ---

// claims достаёт claims из контекста (за authn всегда есть; guard на случай
// неверного монтирования маршрута).
func (h *Handler) claims(w http.ResponseWriter, r *http.Request) (auth.Claims, bool) {
	c, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return auth.Claims{}, false
	}
	return c, true
}

// decodeBody читает JSON-тело (лимит 64 KiB); при ошибке пишет VALIDATION_ERROR.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, rid string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return false
	}
	return true
}

func inviteBody(res InviteResult) inviteJSON {
	return inviteJSON{
		Token:     res.Token,
		URL:       res.URL,
		ExpiresAt: res.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func residentsBody(rows []Resident) []residentJSON {
	out := make([]residentJSON, 0, len(rows))
	for _, res := range rows {
		apts := make([]apartmentJSON, 0, len(res.Apartments))
		for _, a := range res.Apartments {
			apts = append(apts, apartmentJSON{ID: a.ID, Number: a.Number, Role: a.Role})
		}
		grants := make([]grantJSON, 0, len(res.Grants))
		for _, g := range res.Grants {
			grants = append(grants, grantJSON{PublicID: g.PublicID, Label: g.Label})
		}
		out = append(out, residentJSON{
			UserID:     res.UserID,
			Phone:      res.Phone,
			Kind:       res.Kind,
			Apartments: apts,
			Grants:     grants,
		})
	}
	return out
}
