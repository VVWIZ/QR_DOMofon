package onboarding

// Прикладная логика онбординга (онбординг + гранты): создание инвайтов
// владельцев/жильцов, выдача грантов на калитки/шлагбаумы, приём инвайта и
// выборка жильцов для УК. RBAC-скоуп: УК — по своей mc; владелец — только своя
// квартира. Мок доставки инвайта: секрет-ссылка возвращается в ответе.
//
// Секреты (raw-токен инвайта) НИКОГДА не попадают в аудит и логи — только факт
// события и его объекты.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// InviteTTL — срок жизни инвайт-ссылки (скоуп: 7 дней).
const InviteTTL = 7 * 24 * time.Hour

// TokenIssuer выпускает пару токенов для пользователя по id (вход без OTP при
// приёме инвайта НОВЫМ пользователем). Реализация — auth.Service.IssueForUser.
type TokenIssuer interface {
	IssueForUser(ctx context.Context, userID string) (auth.LoginResult, *httpx.Error)
}

// Service — доменная логика онбординга.
type Service struct {
	repo    *Repo
	issuer  TokenIssuer
	baseURL string // база инвайт-ссылки (VISITOR_BASE_URL, без хвостового /)
	audit   audit.Recorder
	log     *slog.Logger
	now     func() time.Time
}

// NewService собирает сервис онбординга.
func NewService(
	repo *Repo,
	issuer TokenIssuer,
	baseURL string,
	recorder audit.Recorder,
	log *slog.Logger,
) *Service {
	return &Service{
		repo:    repo,
		issuer:  issuer,
		baseURL: strings.TrimRight(baseURL, "/"),
		audit:   recorder,
		log:     log,
		now:     time.Now,
	}
}

// InviteResult — выданный инвайт (мок доставки: секрет-ссылка в ответе).
type InviteResult struct {
	Token     string
	URL       string
	ExpiresAt time.Time
}

// GrantResult — результат выдачи гранта на точку: либо грант выдан сразу
// (пользователь уже в УК), либо выпущен инвайт для активации.
type GrantResult struct {
	Granted             bool
	UserID              string
	AccessPointPublicID string
	Invite              *InviteResult
}

// AcceptResult — результат приёма инвайта.
//
// Created=true → пользователь создан этим приёмом: выдаётся сессия (вход без
// OTP, Login валиден). Created=false → пользователь уже существовал: привязка/
// грант созданы, но сессия НЕ выдаётся — иначе кто угодно, зная телефон, мог бы
// выпустить себе сессию за чужого жильца со всеми его ролями (ревью, этап 6).
// Вход — обычным OTP.
type AcceptResult struct {
	UserID  string
	Created bool
	Login   auth.LoginResult
}

// CreateOwnerInvite (УК-админ) создаёт инвайт владельца на квартиру своей УК.
func (s *Service) CreateOwnerInvite(ctx context.Context, claims auth.Claims, apartmentID, phone string) (InviteResult, *httpx.Error) {
	phone = NormalizePhone(phone)
	if phone == "" {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Field phone is invalid")
	}
	apt, ok, err := s.repo.GetApartment(ctx, apartmentID)
	if err != nil {
		return InviteResult{}, s.internal("get_apartment_failed", err)
	}
	// Чужая УК и «не найдено» отвечают одинаково — иначе админ мог бы
	// перебором отличать существующие объекты чужих УК.
	if !ok || apt.MCID != claims.MCID {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	return s.createInvite(ctx, claims, NewInvite{
		Phone:       phone,
		TargetKind:  "apartment_owner",
		ApartmentID: apartmentID,
		Role:        "owner",
		MCID:        apt.MCID,
		CreatedBy:   claims.Subject,
	})
}

// CreateResidentInvite (владелец) создаёт инвайт жильца в СВОЮ квартиру.
func (s *Service) CreateResidentInvite(ctx context.Context, claims auth.Claims, apartmentID, phone string) (InviteResult, *httpx.Error) {
	if !CanInviteToApartment(claims, apartmentID) {
		return InviteResult{}, httpx.NewError(httpx.CodeForbidden, "You are not the owner of this apartment")
	}
	phone = NormalizePhone(phone)
	if phone == "" {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Field phone is invalid")
	}
	apt, ok, err := s.repo.GetApartment(ctx, apartmentID)
	if err != nil {
		return InviteResult{}, s.internal("get_apartment_failed", err)
	}
	if !ok {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	return s.createInvite(ctx, claims, NewInvite{
		Phone:       phone,
		TargetKind:  "apartment_resident",
		ApartmentID: apartmentID,
		Role:        "resident",
		MCID:        apt.MCID,
		CreatedBy:   claims.Subject,
	})
}

// CreateAccessGrant (УК-админ) выдаёт доступ на калитку/шлагбаум своей УК. Если
// пользователь с таким телефоном уже в УК — грант выдаётся сразу; иначе
// выпускается инвайт для активации.
func (s *Service) CreateAccessGrant(ctx context.Context, claims auth.Claims, accessPointPublicID, phone string) (GrantResult, *httpx.Error) {
	phone = NormalizePhone(phone)
	if phone == "" {
		return GrantResult{}, httpx.NewError(httpx.CodeValidationError, "Field phone is invalid")
	}
	ap, ok, err := s.repo.GetAccessPoint(ctx, accessPointPublicID)
	if err != nil {
		return GrantResult{}, s.internal("get_access_point_failed", err)
	}
	// Чужая УК = «не найдено» (см. CreateOwnerInvite).
	if !ok || ap.MCID != claims.MCID {
		return GrantResult{}, httpx.NewError(httpx.CodeValidationError, "Access point not found")
	}
	if ap.Type != "gate" && ap.Type != "barrier" {
		return GrantResult{}, httpx.NewError(httpx.CodeValidationError, "Access point is not a gate or barrier")
	}

	userID, found, err := s.repo.FindUserByPhoneInMC(ctx, phone, ap.MCID)
	if err != nil {
		return GrantResult{}, s.internal("find_user_in_mc_failed", err)
	}
	if found {
		if err := s.repo.AttachAccessGrant(ctx, userID, ap.ID, ap.MCID, claims.Subject); err != nil {
			return GrantResult{}, s.internal("attach_grant_failed", err)
		}
		s.record(ctx, audit.Event{
			EventType:           "access_grant_created",
			Actor:               "user:" + claims.Subject,
			AccessPointID:       ap.ID,
			ManagementCompanyID: ap.MCID,
			Metadata:            map[string]any{"granted_to": userID, "direct": true},
		})
		return GrantResult{Granted: true, UserID: userID, AccessPointPublicID: accessPointPublicID}, nil
	}

	inv, apiErr := s.createInvite(ctx, claims, NewInvite{
		Phone:         phone,
		TargetKind:    "access_grant",
		AccessPointID: ap.ID,
		MCID:          ap.MCID,
		CreatedBy:     claims.Subject,
	})
	if apiErr != nil {
		return GrantResult{}, apiErr
	}
	return GrantResult{Granted: false, Invite: &inv}, nil
}

// AcceptInvite принимает инвайт по секрет-токену: создаёт/находит пользователя и
// привязку/грант. Сессия выдаётся только НОВОМУ пользователю (см. AcceptResult).
func (s *Service) AcceptInvite(ctx context.Context, rawToken string) (AcceptResult, *httpx.Error) {
	if rawToken == "" {
		return AcceptResult{}, httpx.NewError(httpx.CodeValidationError, "Field token is required")
	}
	acc, apiErr := s.repo.AcceptInviteTx(ctx, HashToken(rawToken), s.now())
	if apiErr != nil {
		return AcceptResult{}, apiErr
	}

	s.record(ctx, audit.Event{
		EventType:           "invite_accepted",
		Actor:               "user:" + acc.UserID,
		ApartmentID:         acc.ApartmentID,
		AccessPointID:       acc.AccessPointID,
		ManagementCompanyID: acc.MCID,
		Metadata:            map[string]any{"target_kind": acc.TargetKind, "user_created": acc.Created},
	})

	if !acc.Created {
		// Существующий пользователь: привязка создана, сессия по ссылке не выдаётся.
		return AcceptResult{UserID: acc.UserID, Created: false}, nil
	}
	login, apiErr := s.issuer.IssueForUser(ctx, acc.UserID)
	if apiErr != nil {
		return AcceptResult{}, apiErr
	}
	return AcceptResult{UserID: acc.UserID, Created: true, Login: login}, nil
}

// ListResidents (УК-админ) возвращает всех жильцов/владельцев своей УК.
func (s *Service) ListResidents(ctx context.Context, claims auth.Claims) ([]Resident, *httpx.Error) {
	res, err := s.repo.ListResidents(ctx, claims.MCID)
	if err != nil {
		return nil, s.internal("list_residents_failed", err)
	}
	return res, nil
}

// createInvite генерирует секрет-токен, сохраняет хеш-инвайт, пишет аудит и
// возвращает секрет-ссылку (мок доставки). Сам токен в аудит не попадает.
func (s *Service) createInvite(ctx context.Context, claims auth.Claims, in NewInvite) (InviteResult, *httpx.Error) {
	raw, err := GenerateToken()
	if err != nil {
		return InviteResult{}, s.internal("generate_invite_token_failed", err)
	}
	in.TokenHash = HashToken(raw)
	in.ExpiresAt = s.now().Add(InviteTTL)
	if err := s.repo.CreateInvite(ctx, in); err != nil {
		return InviteResult{}, s.internal("create_invite_failed", err)
	}

	s.record(ctx, audit.Event{
		EventType:           "invite_created",
		Actor:               "user:" + claims.Subject,
		ApartmentID:         in.ApartmentID,
		AccessPointID:       in.AccessPointID,
		ManagementCompanyID: in.MCID,
		Metadata: map[string]any{
			"target_kind": in.TargetKind,
			"role":        in.Role,
			"expires_at":  in.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})

	return InviteResult{Token: raw, URL: s.baseURL + "/invite/" + raw, ExpiresAt: in.ExpiresAt}, nil
}

// record — best-effort аудит (ошибка логируется, флоу не валит).
func (s *Service) record(ctx context.Context, ev audit.Event) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, ev); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
	}
}

// internal логирует внутреннюю ошибку и отдаёт наружу нейтральный INTERNAL.
func (s *Service) internal(msg string, err error) *httpx.Error {
	s.log.Error(msg, "error", err)
	return httpx.NewError(httpx.CodeInternal, "Internal server error")
}
