package calls

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/platform/httpx"
)

// Handler обслуживает REST-эндпоинты звонков.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер звонков.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// initiateRequest — тело POST /calls/initiate (то же, что qr/validate).
type initiateRequest struct {
	Aid string `json:"aid"`
	V   string `json:"v"`
	Kid string `json:"kid"`
	Sig string `json:"sig"`
}

type initiateResponse struct {
	CallID       string `json:"call_id"`
	Room         string `json:"room"`
	LiveKitURL   string `json:"livekit_url"`
	VisitorToken string `json:"visitor_token"`
	DeviceStatus string `json:"device_status"`
}

type acceptResponse struct {
	Room          string `json:"room"`
	LiveKitURL    string `json:"livekit_url"`
	ResidentToken string `json:"resident_token"`
}

// Initiate — POST /api/v1/calls/initiate.
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())

	var req initiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return
	}
	if req.Aid == "" || req.V == "" || req.Kid == "" || req.Sig == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields aid, v, kid, sig are required", rid)
		return
	}

	res, apiErr := h.svc.Initiate(r.Context(), ValidateInput{Aid: req.Aid, V: req.V, Kid: req.Kid, Sig: req.Sig})
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, initiateResponse{
		CallID:       res.CallID,
		Room:         res.Room,
		LiveKitURL:   res.LiveKitURL,
		VisitorToken: res.VisitorToken,
		DeviceStatus: res.DeviceStatus,
	})
}

// Accept — POST /api/v1/calls/{id}/accept.
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "id")
	res, apiErr := h.svc.Accept(r.Context(), callID)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, acceptResponse{
		Room:          res.Room,
		LiveKitURL:    res.LiveKitURL,
		ResidentToken: res.ResidentToken,
	})
}

// Cancel — POST /api/v1/calls/{id}/cancel (204).
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "id")
	if apiErr := h.svc.Cancel(r.Context(), callID); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// End — POST /api/v1/calls/{id}/end (204).
func (h *Handler) End(w http.ResponseWriter, r *http.Request) {
	callID := chi.URLParam(r, "id")
	if apiErr := h.svc.End(r.Context(), callID); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
