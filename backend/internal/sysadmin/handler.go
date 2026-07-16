package sysadmin

// HTTP-хендлеры /system/* — за authn + RequireSystemAdmin (проверка роли в
// middleware). Создание mc_admin возвращает otpauth://-URI один раз.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает /api/v1/system/*.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- тела запросов ---

type createMCRequest struct {
	Name string `json:"name"`
}
type createMCAdminRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Password string `json:"password"`
}
type createSiteRequest struct {
	MCID    string `json:"mc_id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
}
type createBuildingRequest struct {
	SiteID  string `json:"site_id"`
	Address string `json:"address"`
}
type createEntranceRequest struct {
	BuildingID string `json:"building_id"`
	Number     string `json:"number"`
}
type moveBuildingRequest struct {
	SiteID string `json:"site_id"`
}

// ListMCs — GET /system/management-companies.
func (h *Handler) ListMCs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r); !ok {
		return
	}
	rows, apiErr := h.svc.ListMCs(r.Context())
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{"id": m.ID, "name": m.Name, "sites": m.Sites, "buildings": m.Buildings})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"management_companies": out})
}

// CreateMC — POST /system/management-companies.
func (h *Handler) CreateMC(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createMCRequest
	if !decodeBody(w, r, &req) {
		return
	}
	id, apiErr := h.svc.CreateMC(r.Context(), actor, req.Name)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// CreateMCAdmin — POST /system/management-companies/{mc_id}/admins.
func (h *Handler) CreateMCAdmin(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createMCAdminRequest
	if !decodeBody(w, r, &req) {
		return
	}
	res, apiErr := h.svc.CreateMCAdmin(r.Context(), actor, chi.URLParam(r, "mc_id"), req.Email, req.FullName, req.Password)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	// otpauth_url — показать администратору один раз (завести в аутентификатор).
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"user_id": res.UserID, "otpauth_url": res.OTPAuthURL})
}

// Catalog — GET /system/management-companies/{mc_id}/catalog.
func (h *Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r); !ok {
		return
	}
	sites, apiErr := h.svc.Catalog(r.Context(), chi.URLParam(r, "mc_id"))
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sites": sitesBody(sites)})
}

// CreateSite — POST /system/sites.
func (h *Handler) CreateSite(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createSiteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	id, apiErr := h.svc.CreateSite(r.Context(), actor, req.MCID, req.Name, req.Address, req.Kind)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// CreateBuilding — POST /system/buildings.
func (h *Handler) CreateBuilding(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createBuildingRequest
	if !decodeBody(w, r, &req) {
		return
	}
	id, apiErr := h.svc.CreateBuilding(r.Context(), actor, req.SiteID, req.Address)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// CreateEntrance — POST /system/entrances.
func (h *Handler) CreateEntrance(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req createEntranceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	id, apiErr := h.svc.CreateEntrance(r.Context(), actor, req.BuildingID, req.Number)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// MoveBuilding — PATCH /system/buildings/{building_id}.
func (h *Handler) MoveBuilding(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req moveBuildingRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if apiErr := h.svc.MoveBuilding(r.Context(), actor, chi.URLParam(r, "building_id"), req.SiteID); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"moved": true})
}

// --- вспомогательное ---

// actor возвращает id system_admin из claims (за middleware всегда есть).
func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (string, bool) {
	c, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return "", false
	}
	return c.Subject, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	rid := httpx.RequestIDFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return false
	}
	return true
}

func sitesBody(sites []SiteRow) []map[string]any {
	out := make([]map[string]any, 0, len(sites))
	for _, s := range sites {
		buildings := make([]map[string]any, 0, len(s.Buildings))
		for _, b := range s.Buildings {
			entrances := make([]map[string]any, 0, len(b.Entrances))
			for _, e := range b.Entrances {
				entrances = append(entrances, map[string]any{"id": e.ID, "number": e.Number})
			}
			buildings = append(buildings, map[string]any{"id": b.ID, "address": b.Address, "entrances": entrances})
		}
		out = append(out, map[string]any{"id": s.ID, "name": s.Name, "address": s.Address, "kind": s.Kind, "buildings": buildings})
	}
	return out
}
