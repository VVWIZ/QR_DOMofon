package onboarding

// Прикладная логика онбординга (онбординг + гранты): создание инвайтов
// владельцев/жильцов, выдача грантов на калитки/шлагбаумы, приём инвайта (вход
// без OTP) и выборка жильцов для УК. RBAC-скоуп: УК — по своей mc; владелец —
// только своя квартира. Мок доставки инвайта: секрет-ссылка возвращается в ответе.

import (
	"context"
	"strings"
	"time"

	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// InviteTTL — срок жизни инвайт-ссылки (ТЗ/скоуп: 7 дней).
const InviteTTL = 7 * 24 * time.Hour

// TokenIssuer выпускает пару токенов для пользователя по id (вход без OTP при
// приёме инвайта). Реализация — auth.Service.IssueForUser.
type TokenIssuer interface {
	IssueForUser(ctx context.Context, userID string) (auth.LoginResult, *httpx.Error)
}

// Service — доменная логика онбординга.
type Service struct {
	repo    *Repo
	issuer  TokenIssuer
	baseURL string // база инвайт-ссылки (VISITOR_BASE_URL, без хвостового /)
	now     func() time.Time
}

// NewService собирает сервис онбординга.
func NewService(repo *Repo, issuer TokenIssuer, baseURL string) *Service {
	return &Service{
		repo:    repo,
		issuer:  issuer,
		baseURL: strings.TrimRight(baseURL, "/"),
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

// CreateOwnerInvite (УК-админ) создаёт инвайт владельца на квартиру своей УК.
func (s *Service) CreateOwnerInvite(ctx context.Context, claims auth.Claims, apartmentID, phone string) (InviteResult, *httpx.Error) {
	apt, ok, err := s.repo.GetApartment(ctx, apartmentID)
	if err != nil {
		return InviteResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	if apt.MCID != claims.MCID {
		return InviteResult{}, httpx.NewError(httpx.CodeForbidden, "Apartment belongs to another management company")
	}
	return s.createInvite(ctx, NewInvite{
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
	apt, ok, err := s.repo.GetApartment(ctx, apartmentID)
	if err != nil {
		return InviteResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return InviteResult{}, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	return s.createInvite(ctx, NewInvite{
		Phone:       phone,
		TargetKind:  "apartment_resident",
		ApartmentID: apartmentID,
		Role:        "resident",
		MCID:        apt.MCID,
		CreatedBy:   claims.Subject,
	})
}

// CreateAccessGrant (УК-админ) выдаёт доступ на калитку/шлагбаум своей УК. Если
// пользователь с таким телефоном уже в УК — грант выдаётся сразу; иначе выпускается
// инвайт для активации.
func (s *Service) CreateAccessGrant(ctx context.Context, claims auth.Claims, accessPointPublicID, phone string) (GrantResult, *httpx.Error) {
	ap, ok, err := s.repo.GetAccessPoint(ctx, accessPointPublicID)
	if err != nil {
		return GrantResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if !ok {
		return GrantResult{}, httpx.NewError(httpx.CodeValidationError, "Access point not found")
	}
	if ap.MCID != claims.MCID {
		return GrantResult{}, httpx.NewError(httpx.CodeForbidden, "Access point belongs to another management company")
	}
	if ap.Type != "gate" && ap.Type != "barrier" {
		return GrantResult{}, httpx.NewError(httpx.CodeValidationError, "Access point is not a gate or barrier")
	}

	userID, found, err := s.repo.FindUserByPhoneInMC(ctx, phone, ap.MCID)
	if err != nil {
		return GrantResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if found {
		if err := s.repo.AttachAccessGrant(ctx, userID, ap.ID, ap.MCID, claims.Subject); err != nil {
			return GrantResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
		}
		return GrantResult{Granted: true, UserID: userID, AccessPointPublicID: accessPointPublicID}, nil
	}

	inv, apiErr := s.createInvite(ctx, NewInvite{
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

// AcceptInvite принимает инвайт по секрет-токену: создаёт/находит пользователя,
// привязку/грант и СРАЗУ выдаёт пару токенов (вход без OTP).
func (s *Service) AcceptInvite(ctx context.Context, rawToken string) (auth.LoginResult, *httpx.Error) {
	if rawToken == "" {
		return auth.LoginResult{}, httpx.NewError(httpx.CodeValidationError, "Field token is required")
	}
	userID, apiErr := s.repo.AcceptInviteTx(ctx, HashToken(rawToken), s.now())
	if apiErr != nil {
		return auth.LoginResult{}, apiErr
	}
	return s.issuer.IssueForUser(ctx, userID)
}

// ListResidents (УК-админ) возвращает всех жильцов/владельцев своей УК.
func (s *Service) ListResidents(ctx context.Context, claims auth.Claims) ([]Resident, *httpx.Error) {
	res, err := s.repo.ListResidents(ctx, claims.MCID)
	if err != nil {
		return nil, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	return res, nil
}

// createInvite генерирует секрет-токен, сохраняет хеш-инвайт и возвращает
// секрет-ссылку (мок доставки).
func (s *Service) createInvite(ctx context.Context, in NewInvite) (InviteResult, *httpx.Error) {
	raw, err := GenerateToken()
	if err != nil {
		return InviteResult{}, httpx.NewError(httpx.CodeInternal, "Failed to generate invite")
	}
	in.TokenHash = HashToken(raw)
	in.ExpiresAt = s.now().Add(InviteTTL)
	if err := s.repo.CreateInvite(ctx, in); err != nil {
		return InviteResult{}, httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	return InviteResult{Token: raw, URL: s.baseURL + "/invite/" + raw, ExpiresAt: in.ExpiresAt}, nil
}
