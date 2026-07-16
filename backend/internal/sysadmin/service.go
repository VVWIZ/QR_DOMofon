package sysadmin

// Прикладная логика платформенной админки. Скоуп — только system_admin (проверен
// middleware RequireSystemAdmin, не ограничен mc). Пароли хешируются bcrypt,
// TOTP-секрет нового УК-админа генерируется сервером и отдаётся как otpauth://-URI
// ОДИН раз (в аудит/логи не пишется).

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Service — доменная логика sysadmin.
type Service struct {
	repo  *Repo
	audit audit.Recorder
	log   *slog.Logger
}

// NewService собирает сервис.
func NewService(repo *Repo, recorder audit.Recorder, log *slog.Logger) *Service {
	return &Service{repo: repo, audit: recorder, log: log}
}

// AdminCreated — результат создания УК-админа: otpauth://-URI отдаётся один раз.
type AdminCreated struct {
	UserID     string
	OTPAuthURL string
}

func (s *Service) ListMCs(ctx context.Context) ([]MCRow, *httpx.Error) {
	out, err := s.repo.ListMCs(ctx)
	if err != nil {
		return nil, s.internal("list_mcs_failed", err)
	}
	return out, nil
}

func (s *Service) CreateMC(ctx context.Context, actor, name string) (string, *httpx.Error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", httpx.NewError(httpx.CodeValidationError, "Field name is required")
	}
	id, err := s.repo.CreateMC(ctx, name)
	if apiErr := s.mapErr(err, "create_mc_failed"); apiErr != nil {
		return "", apiErr
	}
	s.record(ctx, audit.Event{EventType: "mc_created", Actor: "system_admin:" + actor, ManagementCompanyID: id, Metadata: map[string]any{"name": name}})
	return id, nil
}

func (s *Service) CreateMCAdmin(ctx context.Context, actor, mcID, email, fullName, password string) (AdminCreated, *httpx.Error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || len(password) < 8 {
		return AdminCreated{}, httpx.NewError(httpx.CodeValidationError, "email and password (≥8 chars) are required")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return AdminCreated{}, s.internal("hash_password_failed", err)
	}
	secret, otpauthURL, err := auth.GenerateTOTPSecret(email)
	if err != nil {
		return AdminCreated{}, s.internal("gen_totp_failed", err)
	}
	id, err := s.repo.CreateMCAdmin(ctx, mcID, email, strings.TrimSpace(fullName), hash, secret)
	if apiErr := s.mapErr(err, "create_mc_admin_failed"); apiErr != nil {
		return AdminCreated{}, apiErr
	}
	// Секрет/otpauth НЕ логируем и НЕ пишем в аудит.
	s.record(ctx, audit.Event{EventType: "mc_admin_created", Actor: "system_admin:" + actor, ManagementCompanyID: mcID, Metadata: map[string]any{"user_id": id, "email": email}})
	return AdminCreated{UserID: id, OTPAuthURL: otpauthURL}, nil
}

func (s *Service) CreateSite(ctx context.Context, actor, mcID, name, address, kind string) (string, *httpx.Error) {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if name == "" || address == "" {
		return "", httpx.NewError(httpx.CodeValidationError, "name and address are required")
	}
	if kind == "" {
		kind = "complex"
	}
	if kind != "complex" && kind != "standalone" {
		return "", httpx.NewError(httpx.CodeValidationError, "kind must be complex or standalone")
	}
	id, err := s.repo.CreateSite(ctx, mcID, name, address, kind)
	if apiErr := s.mapErr(err, "create_site_failed"); apiErr != nil {
		return "", apiErr
	}
	s.record(ctx, audit.Event{EventType: "site_created", Actor: "system_admin:" + actor, ManagementCompanyID: mcID, Metadata: map[string]any{"site_id": id, "name": name, "kind": kind}})
	return id, nil
}

func (s *Service) CreateBuilding(ctx context.Context, actor, siteID, address string) (string, *httpx.Error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", httpx.NewError(httpx.CodeValidationError, "address is required")
	}
	id, err := s.repo.CreateBuilding(ctx, siteID, address)
	if apiErr := s.mapErr(err, "create_building_failed"); apiErr != nil {
		return "", apiErr
	}
	s.record(ctx, audit.Event{EventType: "building_created", Actor: "system_admin:" + actor, Metadata: map[string]any{"building_id": id, "site_id": siteID, "address": address}})
	return id, nil
}

func (s *Service) CreateEntrance(ctx context.Context, actor, buildingID, number string) (string, *httpx.Error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return "", httpx.NewError(httpx.CodeValidationError, "number is required")
	}
	id, err := s.repo.CreateEntrance(ctx, buildingID, number)
	if apiErr := s.mapErr(err, "create_entrance_failed"); apiErr != nil {
		return "", apiErr
	}
	s.record(ctx, audit.Event{EventType: "entrance_created", Actor: "system_admin:" + actor, Metadata: map[string]any{"entrance_id": id, "building_id": buildingID, "number": number}})
	return id, nil
}

func (s *Service) MoveBuilding(ctx context.Context, actor, buildingID, newSiteID string) *httpx.Error {
	err := s.repo.MoveBuilding(ctx, buildingID, newSiteID)
	if apiErr := s.mapErr(err, "move_building_failed"); apiErr != nil {
		return apiErr
	}
	s.record(ctx, audit.Event{EventType: "building_moved", Actor: "system_admin:" + actor, Metadata: map[string]any{"building_id": buildingID, "site_id": newSiteID}})
	return nil
}

func (s *Service) Catalog(ctx context.Context, mcID string) ([]SiteRow, *httpx.Error) {
	out, err := s.repo.Catalog(ctx, mcID)
	if err != nil {
		return nil, s.internal("catalog_failed", err)
	}
	return out, nil
}

// --- helpers ---

// mapErr переводит сентинелы репозитория в httpx-ошибки; nil → nil.
func (s *Service) mapErr(err error, logMsg string) *httpx.Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrConflict):
		return httpx.NewError(httpx.CodeValidationError, "Already exists")
	case errors.Is(err, ErrParentNotFound):
		return httpx.NewError(httpx.CodeValidationError, "Parent not found")
	default:
		return s.internal(logMsg, err)
	}
}

func (s *Service) record(ctx context.Context, ev audit.Event) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, ev); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
	}
}

func (s *Service) internal(msg string, err error) *httpx.Error {
	s.log.Error(msg, "error", err)
	return httpx.NewError(httpx.CodeInternal, "Internal server error")
}
