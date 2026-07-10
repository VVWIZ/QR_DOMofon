package calls

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/platform/httpx"
)

// ErrPropertyNotFound — точка доступа по aid не найдена/неактивна (адаптер
// property→calls в cmd/server конвертирует property.ErrNotFound в неё).
var ErrPropertyNotFound = errors.New("calls: property not found")

// Property — контекст точки доступа, нужный calls (интерфейс на стороне
// потребителя, architecture.md §1).
type Property struct {
	AccessPointID       string
	AccessPointPublicID string
	AccessPointLabel    string
	ApartmentID         string
	ApartmentNumber     string
	DeviceID            string
	ManagementCompanyID string
}

// QRVerifier проверяет подпись QR (реализация оборачивает qr.Verify+keyring).
// Ошибка любая → наружу INVALID_QR (причина не раскрывается).
type QRVerifier interface {
	Verify(aid, v, kid, sig string) error
}

// PropertyResolver разрешает контекст по public_id (= aid).
type PropertyResolver interface {
	ResolveByPublicID(ctx context.Context, publicID string) (Property, error)
}

// PresenceChecker сообщает online-статус устройства.
type PresenceChecker interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
}

// Media — комнаты и токены LiveKit (реализация — *LiveKit).
type Media interface {
	CreateRoom(ctx context.Context, room string) error
	CloseRoom(ctx context.Context, room string) error
	VisitorToken(room, identity string) (string, error)
	ResidentToken(room, identity string) (string, error)
	URL() string
}

// ValidateInput — общее тело initiate (совпадает с qr/validate).
type ValidateInput struct {
	Aid string
	V   string
	Kid string
	Sig string
}

// InitiateResult — результат POST /calls/initiate.
type InitiateResult struct {
	CallID       string
	Room         string
	LiveKitURL   string
	VisitorToken string
	DeviceStatus string
}

// AcceptResult — результат POST /calls/{id}/accept.
type AcceptResult struct {
	Room          string
	LiveKitURL    string
	ResidentToken string
}

// Service — доменная логика звонков.
type Service struct {
	qr       QRVerifier
	property PropertyResolver
	presence PresenceChecker
	media    Media
	notifier Notifier
	sessions *Store
	audit    audit.Recorder
	log      *slog.Logger
}

// NewService собирает сервис звонков.
func NewService(
	qr QRVerifier,
	property PropertyResolver,
	presence PresenceChecker,
	media Media,
	notifier Notifier,
	sessions *Store,
	recorder audit.Recorder,
	log *slog.Logger,
) *Service {
	return &Service{
		qr:       qr,
		property: property,
		presence: presence,
		media:    media,
		notifier: notifier,
		sessions: sessions,
		audit:    recorder,
		log:      log,
	}
}

// Initiate повторяет валидацию QR (не доверяя клиенту), делает busy-check,
// создаёт комнату LiveKit и токен посетителя, сигналит жильцу и пишет аудит.
func (s *Service) Initiate(ctx context.Context, in ValidateInput) (InitiateResult, *httpx.Error) {
	if err := s.qr.Verify(in.Aid, in.V, in.Kid, in.Sig); err != nil {
		s.log.Warn("qr_validation_failed", "reason", err.Error(), "aid", in.Aid, "kid", in.Kid)
		return InitiateResult{}, httpx.NewError(httpx.CodeInvalidQR, "QR is invalid")
	}

	prop, err := s.property.ResolveByPublicID(ctx, in.Aid)
	if errors.Is(err, ErrPropertyNotFound) {
		s.log.Warn("qr_validation_failed", "reason", "property_not_found", "aid", in.Aid)
		return InitiateResult{}, httpx.NewError(httpx.CodeInvalidQR, "QR is invalid")
	}
	if err != nil {
		s.log.Error("property_resolve_failed", "error", err, "aid", in.Aid)
		return InitiateResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}

	callID := uuid.NewString()
	sess := Session{
		CallID:              callID,
		ApartmentID:         prop.ApartmentID,
		AccessPointID:       prop.AccessPointID,
		AccessPointLabel:    prop.AccessPointLabel,
		DeviceID:            prop.DeviceID,
		ManagementCompanyID: prop.ManagementCompanyID,
		State:               "ringing",
	}

	ok, err := s.sessions.Create(ctx, sess)
	if err != nil {
		s.log.Error("session_create_failed", "error", err, "apartment_id", prop.ApartmentID)
		return InitiateResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return InitiateResult{}, httpx.NewError(httpx.CodeCallInProgress, "Apartment is busy with another call")
	}

	// Комната создаётся и автоматически при join; ошибка здесь не фатальна.
	if err := s.media.CreateRoom(ctx, callID); err != nil {
		s.log.Warn("livekit_create_room_failed", "error", err, "room", callID)
	}

	token, err := s.media.VisitorToken(callID, "visitor:"+prop.ApartmentID)
	if err != nil {
		s.log.Error("visitor_token_failed", "error", err, "room", callID)
		_ = s.sessions.Delete(ctx, sess)
		return InitiateResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}

	online, err := s.presence.IsOnline(ctx, prop.DeviceID)
	if err != nil {
		s.log.Error("presence_check_failed", "error", err, "device_id", prop.DeviceID)
		online = false
	}
	status := statusString(online)

	s.notifier.CallIncoming(prop.ApartmentID, CallIncomingPayload{
		CallID:           callID,
		AccessPointLabel: prop.AccessPointLabel,
		ApartmentID:      prop.ApartmentID,
	})

	s.record(ctx, audit.Event{
		EventType:           "call_initiated",
		ApartmentID:         prop.ApartmentID,
		AccessPointID:       prop.AccessPointID,
		DeviceID:            prop.DeviceID,
		CallID:              callID,
		ManagementCompanyID: prop.ManagementCompanyID,
		Metadata:            map[string]any{"device_status": status},
	})

	return InitiateResult{
		CallID:       callID,
		Room:         callID,
		LiveKitURL:   s.media.URL(),
		VisitorToken: token,
		DeviceStatus: status,
	}, nil
}

// Accept выпускает токен жильца, рассылает call.accepted и пишет аудит.
func (s *Service) Accept(ctx context.Context, callID string) (AcceptResult, *httpx.Error) {
	sess, ok, err := s.sessions.Get(ctx, callID)
	if err != nil {
		s.log.Error("session_get_failed", "error", err, "call_id", callID)
		return AcceptResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return AcceptResult{}, httpx.NewError(httpx.CodeCallNotFound, "Call not found or expired")
	}

	token, err := s.media.ResidentToken(callID, "resident:"+sess.ApartmentID)
	if err != nil {
		s.log.Error("resident_token_failed", "error", err, "room", callID)
		return AcceptResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}

	s.notifier.CallAccepted(sess.ApartmentID, callID)

	s.record(ctx, audit.Event{
		EventType:           "call_accepted",
		Actor:               "resident:" + sess.ApartmentID,
		ApartmentID:         sess.ApartmentID,
		AccessPointID:       sess.AccessPointID,
		DeviceID:            sess.DeviceID,
		CallID:              callID,
		ManagementCompanyID: sess.ManagementCompanyID,
	})

	return AcceptResult{Room: callID, LiveKitURL: s.media.URL(), ResidentToken: token}, nil
}

// Cancel — посетитель отменил до ответа: SSE call.cancelled, снятие сессии,
// закрытие комнаты, аудит call_cancelled.
func (s *Service) Cancel(ctx context.Context, callID string) *httpx.Error {
	return s.teardown(ctx, callID, "call_cancelled", true)
}

// End — завершение установленного звонка любой стороной: снятие сессии,
// закрытие комнаты, аудит call_ended.
func (s *Service) End(ctx context.Context, callID string) *httpx.Error {
	return s.teardown(ctx, callID, "call_ended", false)
}

// teardown — общая логика cancel/end (notifyCancelled=true только для cancel).
func (s *Service) teardown(ctx context.Context, callID, eventType string, notifyCancelled bool) *httpx.Error {
	sess, ok, err := s.sessions.Get(ctx, callID)
	if err != nil {
		s.log.Error("session_get_failed", "error", err, "call_id", callID)
		return httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return httpx.NewError(httpx.CodeCallNotFound, "Call not found or expired")
	}

	if notifyCancelled {
		s.notifier.CallCancelled(sess.ApartmentID, callID)
	}
	if err := s.sessions.Delete(ctx, sess); err != nil {
		s.log.Error("session_delete_failed", "error", err, "call_id", callID)
	}
	if err := s.media.CloseRoom(ctx, callID); err != nil {
		s.log.Warn("livekit_close_room_failed", "error", err, "room", callID)
	}

	s.record(ctx, audit.Event{
		EventType:           eventType,
		ApartmentID:         sess.ApartmentID,
		AccessPointID:       sess.AccessPointID,
		DeviceID:            sess.DeviceID,
		CallID:              callID,
		ManagementCompanyID: sess.ManagementCompanyID,
	})
	return nil
}

// record — best-effort запись аудита (ошибка логируется, поток не валит).
func (s *Service) record(ctx context.Context, ev audit.Event) {
	if err := s.audit.Record(ctx, ev); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
	}
}

// statusString переводит online-флаг в строковый статус контракта.
func statusString(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}
