package access

import (
	"encoding/json"
	"net/http"

	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает POST /api/v1/access/open.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер доступа.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// openRequest — тело POST /access/open.
type openRequest struct {
	CallID string `json:"call_id"`
}

type openResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
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
