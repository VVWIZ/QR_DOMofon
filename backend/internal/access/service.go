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
}

// CallStore ищет активную сессию по call_id.
type CallStore interface {
	Lookup(ctx context.Context, callID string) (CallSession, bool, error)
}

// PresenceChecker сообщает online-статус устройства.
type PresenceChecker interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
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

// Service — доменная логика открытия двери.
type Service struct {
	calls     CallStore
	presence  PresenceChecker
	publisher CommandPublisher
	cmdCtx    CommandContextStore
	audit     audit.Recorder
	log       *slog.Logger
}

// NewService собирает сервис доступа.
func NewService(
	calls CallStore,
	presence PresenceChecker,
	publisher CommandPublisher,
	cmdCtx CommandContextStore,
	recorder audit.Recorder,
	log *slog.Logger,
) *Service {
	return &Service{
		calls:     calls,
		presence:  presence,
		publisher: publisher,
		cmdCtx:    cmdCtx,
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
