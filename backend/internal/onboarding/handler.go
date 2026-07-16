package onboarding

// HTTP-хендлеры онбординга (api.md). Приём инвайта — публичный (вход без OTP,
// возвращает пару токенов как логин); остальное — за authn+RBAC (admin/owner),
// проверки скоупа — в сервисе.

import (
	"encoding/json"
	"net/http"
	"strings"
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
	ApartmentID          string   `json:"apartment_id"`
	Phone                string   `json:"phone"`
	FullName             string   `json:"full_name"`
	AccessPointPublicIDs []string `json:"access_point_public_ids"` // опц. доп. гранты (композитный инвайт)
}

type grantRequest struct {
	AccessPointPublicID string `json:"access_point_public_id"`
	Phone               string `json:"phone"`
	FullName            string `json:"full_name"`
}

type residentInviteRequest struct {
	Phone    string `json:"phone"`
	FullName string `json:"full_name"`
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
	FullName   string          `json:"full_name"`
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
	if req.ApartmentID == "" || req.Phone == "" || strings.TrimSpace(req.FullName) == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields apartment_id, phone and full_name are required", rid)
		return
	}
	res, apiErr := h.svc.CreateOwnerInvite(r.Context(), claims, req.ApartmentID, req.Phone, req.FullName, req.AccessPointPublicIDs)
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
	if req.AccessPointPublicID == "" || req.Phone == "" || strings.TrimSpace(req.FullName) == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields access_point_public_id, phone and full_name are required", rid)
		return
	}
	res, apiErr := h.svc.CreateAccessGrant(r.Context(), claims, req.AccessPointPublicID, req.Phone, req.FullName)
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

// ListCatalog — GET /api/v1/admin/catalog (УК-админ): дерево дом→подъезд→квартира
// + точки gate/barrier своей УК (для выпадашек формы).
func (h *Handler) ListCatalog(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	cat, apiErr := h.svc.Catalog(r.Context(), claims)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, catalogBody(cat))
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
	if req.Phone == "" || strings.TrimSpace(req.FullName) == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields phone and full_name are required", rid)
		return
	}
	res, apiErr := h.svc.CreateResidentInvite(r.Context(), claims, apartmentID, req.Phone, req.FullName)
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

// --- Каталог УК ---

type catalogApartmentJSON struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

type catalogEntranceJSON struct {
	ID         string                 `json:"id"`
	Number     string                 `json:"number"`
	Apartments []catalogApartmentJSON `json:"apartments"`
}

type catalogBuildingJSON struct {
	ID        string                `json:"id"`
	Address   string                `json:"address"`
	Entrances []catalogEntranceJSON `json:"entrances"`
}

type catalogPointJSON struct {
	PublicID string `json:"public_id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
}

type catalogJSON struct {
	Buildings []catalogBuildingJSON `json:"buildings"`
	Points    []catalogPointJSON    `json:"points"`
}

func catalogBody(cat Catalog) catalogJSON {
	out := catalogJSON{
		Buildings: make([]catalogBuildingJSON, 0, len(cat.Buildings)),
		Points:    make([]catalogPointJSON, 0, len(cat.Points)),
	}
	for _, b := range cat.Buildings {
		bj := catalogBuildingJSON{ID: b.ID, Address: b.Address, Entrances: make([]catalogEntranceJSON, 0, len(b.Entrances))}
		for _, e := range b.Entrances {
			ej := catalogEntranceJSON{ID: e.ID, Number: e.Number, Apartments: make([]catalogApartmentJSON, 0, len(e.Apartments))}
			for _, a := range e.Apartments {
				ej.Apartments = append(ej.Apartments, catalogApartmentJSON{ID: a.ID, Number: a.Number})
			}
			bj.Entrances = append(bj.Entrances, ej)
		}
		out.Buildings = append(out.Buildings, bj)
	}
	for _, p := range cat.Points {
		out.Points = append(out.Points, catalogPointJSON{PublicID: p.PublicID, Label: p.Label, Type: p.Type})
	}
	return out
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
			FullName:   res.FullName,
			Kind:       res.Kind,
			Apartments: apts,
			Grants:     grants,
		})
	}
	return out
}
