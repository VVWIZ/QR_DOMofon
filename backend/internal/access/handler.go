package access

import (
	"context"
	"encoding/json"
	"net/http"

	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает эндпоинты /api/v1/access/*.
type Handler struct {
	svc *Service
	// subject извлекает id пользователя (claim sub) из контекста запроса —
	// инъекция вместо импорта auth (граница модуля, как MCIDFromContext в audit).
	subject func(context.Context) string
}

// NewHandler создаёт хендлер доступа. subject — извлечение id пользователя из
// контекста (для открытия/листинга точек по гранту); может быть nil, если
// используется только Open (по call_id).
func NewHandler(svc *Service, subject func(context.Context) string) *Handler {
	return &Handler{svc: svc, subject: subject}
}

// openRequest — тело POST /access/open.
type openRequest struct {
	CallID string `json:"call_id"`
}

// openPointRequest — тело POST /access/open-point.
type openPointRequest struct {
	PublicID string `json:"public_id"`
}

type openResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

type pointResponse struct {
	PublicID string `json:"public_id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Online   bool   `json:"online"`
}

type pointsResponse struct {
	Points []pointResponse `json:"points"`
}

// Open — POST /api/v1/access/open.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64 KiB — тело крошечное (L1)
	var req openRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return
	}
	if req.CallID == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field call_id is required", rid)
		return
	}

	res, apiErr := h.svc.Open(r.Context(), req.CallID)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openResponse{RequestID: res.RequestID, Status: res.Status})
}

// ListPoints — GET /api/v1/access/points: точки, на которые у пользователя есть
// грант, с online-статусом (для UI прямого открытия калиток/шлагбаумов).
func (h *Handler) ListPoints(w http.ResponseWriter, r *http.Request) {
	userID := h.subject(r.Context())
	if userID == "" {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return
	}
	res, apiErr := h.svc.ListPoints(r.Context(), userID)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	points := make([]pointResponse, 0, len(res))
	for _, p := range res {
		points = append(points, pointResponse{PublicID: p.PublicID, Label: p.Label, Type: p.Type, Online: p.Online})
	}
	httpx.WriteJSON(w, http.StatusOK, pointsResponse{Points: points})
}

// OpenPoint — POST /api/v1/access/open-point: прямое открытие калитки/шлагбаума
// по постоянному гранту (без звонка).
func (h *Handler) OpenPoint(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	userID := h.subject(r.Context())
	if userID == "" {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", rid)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req openPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return
	}
	if req.PublicID == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field public_id is required", rid)
		return
	}

	res, apiErr := h.svc.OpenPoint(r.Context(), userID, req.PublicID)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openResponse{RequestID: res.RequestID, Status: res.Status})
}
