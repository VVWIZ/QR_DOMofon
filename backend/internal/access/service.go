// Package access — управление доступом: POST /access/open. Проверяет активную
// call-сессию и presence устройства, затем публикует MQTT-команду open_relay
// (architecture.md §1, api.md). Интерфейсы объявлены на стороне потребителя;
// реализации (calls.Store, devices.Commander, devices.Presence,
// devices.CommandContextStore) внедряются адаптерами в cmd/server.
package access

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/platform/httpx"
)

// relayID / durationMs — параметры команды skeleton (PROTOCOL.md §3.1, §6).
const (
	relayID    = 1
	durationMs = 5000
)

// CallSession — минимум данных сессии, нужный access.
type CallSession struct {
	CallID              string
	ApartmentID         string
	AccessPointID       string
	DeviceID            string
	ManagementCompanyID string
	State               string // ringing | accepted (открытие только при accepted)
}

// CallStore ищет активную сессию по call_id.
type CallStore interface {
	Lookup(ctx context.Context, callID string) (CallSession, bool, error)
}

// PresenceChecker сообщает online-статус устройства.
type PresenceChecker interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
}

// Authorizer сверяет привязку текущего пользователя (claims из ctx) к квартире
// сессии. Nil-ошибка = доступ разрешён; иначе → 403 FORBIDDEN. Реализация —
// адаптер в cmd/server поверх auth.ClaimsFromContext + auth.AllowApartment.
type Authorizer interface {
	AllowApartment(ctx context.Context, apartmentID string) error
}

// OpenRelayCommand — команда открытия реле (собирается access, публикуется
// адаптером devices.Commander).
type OpenRelayCommand struct {
	RelayID    int
	DurationMs int
	RequestID  string
	IssuedBy   string
	IssuedAt   string
}

// CommandPublisher публикует команду устройству.
type CommandPublisher interface {
	PublishOpenRelay(ctx context.Context, deviceID string, cmd OpenRelayCommand) error
}

// CommandContextStore сохраняет контекст команды по request_id для последующей
// корреляции command_ack в аудите (реализация — devices.CommandContextStore).
type CommandContextStore interface {
	Save(ctx context.Context, requestID string, meta map[string]string) error
}

// OpenResult — результат POST /access/open.
type OpenResult struct {
	RequestID string
	Status    string
}

// GrantedPoint — точка доступа, на которую у пользователя есть постоянный грант
// (онбординг + гранты). Возвращается PointResolver по (userID, publicID) и несёт
// минимум для публикации команды и аудита открытия по гранту.
type GrantedPoint struct {
	DeviceID            string
	AccessPointID       string
	ApartmentID         string
	ManagementCompanyID string
}

// PointResolver ищет активный грант пользователя на точку по её публичному id.
// ok=false → у пользователя нет гранта на эту точку (→ 403 FORBIDDEN). Реализация
// (pgx-репозиторий грантов) внедряется адаптером в cmd/server.
type PointResolver interface {
	ResolveGrantedPoint(ctx context.Context, userID, publicID string) (GrantedPoint, bool, error)
}

// Service — доменная логика открытия двери.
type Service struct {
	calls     CallStore
	presence  PresenceChecker
	publisher CommandPublisher
	cmdCtx    CommandContextStore
	authz     Authorizer
	audit     audit.Recorder
	log       *slog.Logger

	// resolver — опциональная зависимость OpenPoint (открытие по гранту).
	// Устанавливается сеттером SetPointResolver, чтобы не менять сигнатуру
	// NewService и существующий wiring в cmd/server (финальный wiring — этап
	// backend).
	resolver PointResolver
}

// SetPointResolver внедряет резолвер грантов для OpenPoint. Отдельный сеттер
// (а не параметр NewService) — чтобы существующий вызов NewService и путь Open
// не менялись.
func (s *Service) SetPointResolver(r PointResolver) {
	s.resolver = r
}

// NewService собирает сервис доступа.
func NewService(
	calls CallStore,
	presence PresenceChecker,
	publisher CommandPublisher,
	cmdCtx CommandContextStore,
	authz Authorizer,
	recorder audit.Recorder,
	log *slog.Logger,
) *Service {
	return &Service{
		calls:     calls,
		presence:  presence,
		publisher: publisher,
		cmdCtx:    cmdCtx,
		authz:     authz,
		audit:     recorder,
		log:       log,
	}
}

// Open проверяет активную сессию и presence, публикует open_relay и пишет аудит
// door_open_requested.
func (s *Service) Open(ctx context.Context, callID string) (OpenResult, *httpx.Error) {
	sess, ok, err := s.calls.Lookup(ctx, callID)
	if err != nil {
		s.log.Error("call_lookup_failed", "error", err, "call_id", callID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return OpenResult{}, httpx.NewError(httpx.CodeCallNotFound, "Call not found or expired")
	}

	// RBAC: открыть дверь может только жилец своей квартиры (auth.md §1).
	// Проверяем до presence/publish и независимо от M1-гейта ниже.
	if err := s.authz.AllowApartment(ctx, sess.ApartmentID); err != nil {
		s.log.Warn("open_forbidden", "call_id", callID, "apartment_id", sess.ApartmentID)
		return OpenResult{}, httpx.NewError(httpx.CodeForbidden, "You are not a member of this apartment")
	}

	// M1: дверь открывается только после того, как жилец принял звонок.
	// Без этого владелец валидного QR мог бы открыть себе сам, минуя жильца.
	if sess.State != "accepted" {
		s.log.Warn("open_before_accept", "call_id", callID, "state", sess.State)
		return OpenResult{}, httpx.NewError(httpx.CodeCallNotAccepted, "Call has not been accepted by a resident yet")
	}

	online, err := s.presence.IsOnline(ctx, sess.DeviceID)
	if err != nil {
		s.log.Error("presence_check_failed", "error", err, "device_id", sess.DeviceID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !online {
		return OpenResult{}, httpx.NewError(httpx.CodeDeviceOffline, "Device is offline, door cannot be opened remotely")
	}

	requestID := uuid.NewString()
	issuedBy := "resident:" + sess.ApartmentID
	issuedAt := time.Now().UTC().Format(time.RFC3339)

	cmd := OpenRelayCommand{
		RelayID:    relayID,
		DurationMs: durationMs,
		RequestID:  requestID,
		IssuedBy:   issuedBy,
		IssuedAt:   issuedAt,
	}
	if err := s.publisher.PublishOpenRelay(ctx, sess.DeviceID, cmd); err != nil {
		s.log.Error("publish_open_relay_failed", "error", err, "device_id", sess.DeviceID, "request_id", requestID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Failed to send command")
	}

	// Контекст команды для корреляции command_ack (best-effort).
	if err := s.cmdCtx.Save(ctx, requestID, map[string]string{
		"call_id":               sess.CallID,
		"apartment_id":          sess.ApartmentID,
		"access_point_id":       sess.AccessPointID,
		"device_id":             sess.DeviceID,
		"management_company_id": sess.ManagementCompanyID,
	}); err != nil {
		s.log.Warn("cmd_context_save_failed", "error", err, "request_id", requestID)
	}

	if err := s.audit.Record(ctx, audit.Event{
		EventType:           "door_open_requested",
		Actor:               issuedBy,
		ApartmentID:         sess.ApartmentID,
		AccessPointID:       sess.AccessPointID,
		DeviceID:            sess.DeviceID,
		CallID:              sess.CallID,
		RequestID:           requestID,
		ManagementCompanyID: sess.ManagementCompanyID,
		Metadata:            map[string]any{"relay_id": relayID, "duration_ms": durationMs},
	}); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", "door_open_requested")
	}

	return OpenResult{RequestID: requestID, Status: "sent"}, nil
}

// OpenPoint открывает точку по постоянному гранту пользователя (онбординг +
// гранты), БЕЗ call-сессии и M1-гейта: наличие гранта само по себе даёт право.
// Шаги: резолв гранта (нет → 403 FORBIDDEN), presence устройства (offline → 503
// DEVICE_OFFLINE, publish не вызывается), публикация open_relay, best-effort
// cmdCtx.Save и аудит door_open_requested. Возвращает request_id и status="sent".
func (s *Service) OpenPoint(ctx context.Context, userID, publicID string) (OpenResult, *httpx.Error) {
	gp, ok, err := s.resolver.ResolveGrantedPoint(ctx, userID, publicID)
	if err != nil {
		s.log.Error("resolve_granted_point_failed", "error", err, "user_id", userID, "public_id", publicID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		s.log.Warn("open_point_forbidden", "user_id", userID, "public_id", publicID)
		return OpenResult{}, httpx.NewError(httpx.CodeForbidden, "You do not have access to this point")
	}

	online, err := s.presence.IsOnline(ctx, gp.DeviceID)
	if err != nil {
		s.log.Error("presence_check_failed", "error", err, "device_id", gp.DeviceID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !online {
		return OpenResult{}, httpx.NewError(httpx.CodeDeviceOffline, "Device is offline, door cannot be opened remotely")
	}

	requestID := uuid.NewString()
	issuedBy := "resident:" + userID
	issuedAt := time.Now().UTC().Format(time.RFC3339)

	cmd := OpenRelayCommand{
		RelayID:    relayID,
		DurationMs: durationMs,
		RequestID:  requestID,
		IssuedBy:   issuedBy,
		IssuedAt:   issuedAt,
	}
	if err := s.publisher.PublishOpenRelay(ctx, gp.DeviceID, cmd); err != nil {
		s.log.Error("publish_open_relay_failed", "error", err, "device_id", gp.DeviceID, "request_id", requestID)
		return OpenResult{}, httpx.NewError(httpx.CodeInternal, "Failed to send command")
	}

	// Контекст команды для корреляции command_ack (best-effort).
	if err := s.cmdCtx.Save(ctx, requestID, map[string]string{
		"apartment_id":          gp.ApartmentID,
		"access_point_id":       gp.AccessPointID,
		"device_id":             gp.DeviceID,
		"management_company_id": gp.ManagementCompanyID,
	}); err != nil {
		s.log.Warn("cmd_context_save_failed", "error", err, "request_id", requestID)
	}

	if err := s.audit.Record(ctx, audit.Event{
		EventType:           "door_open_requested",
		Actor:               issuedBy,
		AccessPointID:       gp.AccessPointID,
		DeviceID:            gp.DeviceID,
		RequestID:           requestID,
		ManagementCompanyID: gp.ManagementCompanyID,
		Metadata:            map[string]any{"relay_id": relayID, "duration_ms": durationMs},
	}); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", "door_open_requested")
	}

	return OpenResult{RequestID: requestID, Status: "sent"}, nil
}
