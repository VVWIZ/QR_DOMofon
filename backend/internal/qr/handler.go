package qr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"domofon/backend/internal/platform/httpx"
)

// offlineWarning — текст предупреждения при offline-устройстве (api.md, E1).
// Звонок при этом НЕ блокируется — только warning.
const offlineWarning = "Устройство временно недоступно. Вы можете позвонить жильцу — он откроет дверь, когда связь восстановится."

// ErrPropertyNotFound — точка доступа по aid не найдена/неактивна. Объявлена на
// стороне потребителя (qr): адаптер property→qr в cmd/server конвертирует в неё
// property.ErrNotFound. Наружу маппится в INVALID_QR (причина не раскрывается).
var ErrPropertyNotFound = errors.New("qr: property not found")

// ResolvedProperty — минимум, нужный qr-хендлеру от property (границы модулей:
// интерфейс на стороне потребителя, architecture.md §1).
type ResolvedProperty struct {
	AccessPointPublicID string
	AccessPointLabel    string
	ApartmentID         string
	ApartmentNumber     string
	DeviceID            string
}

// PropertyResolver разрешает контекст точки доступа по public_id (= aid).
type PropertyResolver interface {
	ResolveByPublicID(ctx context.Context, publicID string) (ResolvedProperty, error)
}

// PresenceChecker сообщает online-статус устройства (Redis-presence).
type PresenceChecker interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
}

// Handler обслуживает POST /api/v1/qr/validate.
type Handler struct {
	keyring  Keyring
	property PropertyResolver
	presence PresenceChecker
}

// NewHandler создаёт хендлер qr/validate.
func NewHandler(keyring Keyring, property PropertyResolver, presence PresenceChecker) *Handler {
	return &Handler{keyring: keyring, property: property, presence: presence}
}

// validateRequest — тело POST /qr/validate.
type validateRequest struct {
	Aid string `json:"aid"`
	V   string `json:"v"`
	Kid string `json:"kid"`
	Sig string `json:"sig"`
}

type accessPointJSON struct {
	PublicID string `json:"public_id"`
	Label    string `json:"label"`
}

type apartmentJSON struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

type validateResponse struct {
	AccessPoint  accessPointJSON `json:"access_point"`
	Apartment    apartmentJSON   `json:"apartment"`
	DeviceStatus string          `json:"device_status"`
	Warning      string          `json:"warning,omitempty"`
}

// Validate проверяет подпись QR и возвращает контекст точки доступа + статус
// устройства. Причина отказа не раскрывается клиенту (логируется как
// qr_validation_failed), наружу — единый INVALID_QR.
func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := httpx.LoggerFromContext(ctx)
	rid := httpx.RequestIDFromContext(ctx)

	var req validateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return
	}
	if req.Aid == "" || req.V == "" || req.Kid == "" || req.Sig == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields aid, v, kid, sig are required", rid)
		return
	}

	if err := Verify(req.Aid, req.V, req.Kid, req.Sig, h.keyring); err != nil {
		log.Warn("qr_validation_failed", "reason", err.Error(), "aid", req.Aid, "kid", req.Kid)
		httpx.WriteError(w, httpx.CodeInvalidQR, "QR is invalid", rid)
		return
	}

	prop, err := h.property.ResolveByPublicID(ctx, req.Aid)
	if errors.Is(err, ErrPropertyNotFound) {
		log.Warn("qr_validation_failed", "reason", "property_not_found", "aid", req.Aid)
		httpx.WriteError(w, httpx.CodeInvalidQR, "QR is invalid", rid)
		return
	}
	if err != nil {
		log.Error("qr_property_resolve_failed", "error", err, "aid", req.Aid)
		httpx.WriteError(w, httpx.CodeInternal, "Internal server error", rid)
		return
	}

	online, err := h.presence.IsOnline(ctx, prop.DeviceID)
	if err != nil {
		log.Error("presence_check_failed", "error", err, "device_id", prop.DeviceID)
		online = false
	}

	resp := validateResponse{
		AccessPoint:  accessPointJSON{PublicID: prop.AccessPointPublicID, Label: prop.AccessPointLabel},
		Apartment:    apartmentJSON{ID: prop.ApartmentID, Number: prop.ApartmentNumber},
		DeviceStatus: statusString(online),
	}
	if !online {
		resp.Warning = offlineWarning
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// statusString переводит online-флаг в строковый статус контракта.
func statusString(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}
