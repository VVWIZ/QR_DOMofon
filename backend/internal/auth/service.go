package auth

// Service — прикладная логика auth-эндпоинтов (auth.md §5, api.md): OTP-логин
// жильца/владельца, одношаговый логин УК-админа, ротация refresh, logout, профиль.
// Собирает Claims из repo, подписывает пары токенов (Sign, этап 5a), ведёт
// whitelist refresh-jti и best-effort аудит. Секреты/OTP-коды не логируются.

import (
	"context"
	"log/slog"
	"time"

	"crypto/rsa"

	"github.com/google/uuid"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/platform/httpx"
)

// Service держит зависимости auth-флоу.
type Service struct {
	repo      *Repo
	otp       *OtpService
	whitelist *RefreshWhitelist
	priv      *rsa.PrivateKey
	pub       *rsa.PublicKey
	audit     audit.Recorder
	devMode   bool
	log       *slog.Logger
	now       func() time.Time
}

// NewService собирает auth-сервис. devMode управляет раскрытием dev_code в
// otp/send (в проде поля нет).
func NewService(
	repo *Repo,
	otp *OtpService,
	whitelist *RefreshWhitelist,
	priv *rsa.PrivateKey,
	pub *rsa.PublicKey,
	recorder audit.Recorder,
	devMode bool,
	log *slog.Logger,
) *Service {
	return &Service{
		repo:      repo,
		otp:       otp,
		whitelist: whitelist,
		priv:      priv,
		pub:       pub,
		audit:     recorder,
		devMode:   devMode,
		log:       log,
		now:       time.Now,
	}
}

// ApartmentInfo — квартира пользователя в ответе (api.md user.apartments).
type ApartmentInfo struct {
	ID   string
	Role string
}

// UserProfile — публичный профиль пользователя (тело ответов otp/verify,
// admin/login, me).
type UserProfile struct {
	ID         string
	Kind       Kind
	Apartments []ApartmentInfo
	MCID       string // "" → null в JSON
}

// LoginResult — результат otp/verify и admin/login (access в теле, refresh —
// только в cookie).
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	User         UserProfile
}

// RefreshResult — результат ротации (новый access + новый refresh для cookie).
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
}

// OtpSend выдаёт OTP-код (делегирует политику OtpService). dev_code остаётся в
// результате только в dev-режиме.
func (s *Service) OtpSend(ctx context.Context, phone string) (SendResult, *httpx.Error) {
	res, apiErr := s.otp.Send(ctx, phone, s.now())
	if apiErr != nil {
		return SendResult{}, apiErr
	}
	if !s.devMode {
		res.DevCode = ""
	}
	return res, nil
}

// OtpVerify проверяет код и, при успехе, выпускает пару токенов для
// существующего жильца/владельца. Неизвестный телефон → UNAUTHORIZED (онбординг
// вне скоупа, auth.md §5).
func (s *Service) OtpVerify(ctx context.Context, phone, code string) (LoginResult, *httpx.Error) {
	if apiErr := s.otp.Verify(ctx, phone, code, s.now()); apiErr != nil {
		if apiErr.Code == httpx.CodeUnauthorized {
			s.record(ctx, audit.Event{EventType: "otp_failed", Metadata: map[string]any{"phone": phone}})
		}
		return LoginResult{}, apiErr
	}

	user, err := s.repo.GetUserByPhone(ctx, phone)
	if err != nil {
		// Код верный, но пользователя нет — не раскрываем деталь, отдаём 401.
		return LoginResult{}, httpx.NewError(httpx.CodeUnauthorized, "invalid or expired code")
	}

	return s.issueLogin(ctx, user)
}

// IssueForUser выпускает пару токенов для существующего пользователя по его id —
// вход без OTP/пароля (приём инвайта онбординга, осознанное упрощение bearer-
// ссылки). Переиспользует issueLogin (роли → пара → whitelist → аудит user_login).
// Неизвестный id → UNAUTHORIZED (детали не раскрываются).
func (s *Service) IssueForUser(ctx context.Context, userID string) (LoginResult, *httpx.Error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return LoginResult{}, httpx.NewError(httpx.CodeUnauthorized, "user not found")
	}
	return s.issueLogin(ctx, user)
}

// AdminLogin — одношаговый вход УК-админа: bcrypt-пароль + TOTP. Любая неверная
// часть → единый UNAUTHORIZED без указания какой (auth.md §5).
func (s *Service) AdminLogin(ctx context.Context, email, password, totpCode string) (LoginResult, *httpx.Error) {
	unauth := httpx.NewError(httpx.CodeUnauthorized, "invalid credentials")

	user, err := s.repo.GetAdminByEmail(ctx, email)
	if err != nil {
		return LoginResult{}, unauth
	}
	// Один вход для обоих админ-уровней: mc_admin (УК) и system_admin (платформа).
	// Различение — по kind в claims; UI /admin vs /system гейтит доступ сам.
	if (user.Kind != KindAdmin && user.Kind != KindSystem) || user.PasswordHash == "" || user.TOTPSecret == "" {
		return LoginResult{}, unauth
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return LoginResult{}, unauth
	}
	if !VerifyTOTP(user.TOTPSecret, totpCode, s.now()) {
		return LoginResult{}, unauth
	}

	return s.issueLogin(ctx, user)
}

// Refresh ротирует пару по refresh-токену из cookie: валидирует подпись/typ,
// проверяет и заменяет jti в whitelist (детект reuse), выдаёт новую пару.
// Любой сбой (нет/невалиден/не в whitelist/Redis недоступен) → UNAUTHORIZED
// (fail-closed, auth.md §3).
func (s *Service) Refresh(ctx context.Context, refreshToken string) (RefreshResult, *httpx.Error) {
	if refreshToken == "" {
		return RefreshResult{}, httpx.NewError(httpx.CodeUnauthorized, "missing refresh token")
	}
	claims, err := Parse(s.pub, refreshToken)
	if err != nil || claims.Type != TypeRefresh {
		return RefreshResult{}, httpx.NewError(httpx.CodeUnauthorized, "invalid refresh token")
	}

	access, refresh, newJTI, err := s.signPair(claims.Subject, claims.Kind, claims.Roles, claims.MCID)
	if err != nil {
		s.log.Error("sign_pair_failed", "error", err)
		return RefreshResult{}, httpx.NewError(httpx.CodeInternal, "failed to issue tokens")
	}

	if err := s.whitelist.Rotate(ctx, claims.JTI, newJTI, claims.Subject, RefreshTTL); err != nil {
		// jti нет в whitelist (reuse/отзыв) либо Redis недоступен → fail-closed.
		return RefreshResult{}, httpx.NewError(httpx.CodeUnauthorized, "refresh token is no longer valid")
	}

	s.record(ctx, audit.Event{EventType: "token_refreshed", Actor: "user:" + claims.Subject, ManagementCompanyID: claims.MCID})
	return RefreshResult{AccessToken: access, RefreshToken: refresh}, nil
}

// Logout отзывает refresh (DEL whitelist) и сообщает вызывающему очистить cookie.
// Идемпотентно: ошибки парсинга/Redis не мешают вернуть 204 (access остаётся
// валиден до истечения TTL — stateless).
func (s *Service) Logout(ctx context.Context, refreshToken string) {
	if refreshToken == "" {
		return
	}
	claims, err := Parse(s.pub, refreshToken)
	if err != nil {
		return
	}
	if err := s.whitelist.Revoke(ctx, claims.JTI); err != nil {
		s.log.Warn("refresh_revoke_failed", "error", err)
	}
}

// Me собирает профиль из claims текущего запроса (без похода в БД, auth.md §me).
func (s *Service) Me(claims Claims) UserProfile {
	return profileFromClaims(claims)
}

// issueLogin — общий хвост otp/verify и admin/login: собрать роли, подписать
// пару, занести refresh-jti в whitelist, записать аудит user_login.
func (s *Service) issueLogin(ctx context.Context, user User) (LoginResult, *httpx.Error) {
	roles, err := s.repo.GetRolesByUserID(ctx, user.ID)
	if err != nil {
		s.log.Error("get_roles_failed", "error", err, "user_id", user.ID)
		return LoginResult{}, httpx.NewError(httpx.CodeInternal, "failed to load user roles")
	}
	claimRoles := toClaimRoles(roles)

	access, refresh, jti, err := s.signPair(user.ID, user.Kind, claimRoles, user.MCID)
	if err != nil {
		s.log.Error("sign_pair_failed", "error", err, "user_id", user.ID)
		return LoginResult{}, httpx.NewError(httpx.CodeInternal, "failed to issue tokens")
	}
	if err := s.whitelist.Issue(ctx, jti, user.ID, RefreshTTL); err != nil {
		s.log.Error("refresh_whitelist_issue_failed", "error", err, "user_id", user.ID)
		return LoginResult{}, httpx.NewError(httpx.CodeInternal, "failed to issue tokens")
	}

	s.record(ctx, audit.Event{
		EventType:           "user_login",
		Actor:               "user:" + user.ID,
		ManagementCompanyID: user.MCID,
		Metadata:            map[string]any{"kind": string(user.Kind)},
	})

	return LoginResult{
		AccessToken:  access,
		RefreshToken: refresh,
		User:         profile(user.ID, user.Kind, claimRoles, user.MCID),
	}, nil
}

// signPair подписывает access (TTL AccessTTL) и refresh (TTL RefreshTTL) с
// раздельными jti; возвращает refresh-jti для whitelist. Whitelist не трогает —
// это делает вызывающий (Issue при логине, Rotate при refresh).
func (s *Service) signPair(userID string, kind Kind, roles []ApartmentRole, mcID string) (access, refresh, refreshJTI string, err error) {
	now := s.now()
	refreshJTI = uuid.NewString()

	access, err = Sign(s.priv, Claims{
		Subject:   userID,
		Kind:      kind,
		Roles:     roles,
		MCID:      mcID,
		JTI:       uuid.NewString(),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(AccessTTL).Unix(),
		Type:      TypeAccess,
	})
	if err != nil {
		return "", "", "", err
	}

	refresh, err = Sign(s.priv, Claims{
		Subject:   userID,
		Kind:      kind,
		Roles:     roles,
		MCID:      mcID,
		JTI:       refreshJTI,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(RefreshTTL).Unix(),
		Type:      TypeRefresh,
	})
	if err != nil {
		return "", "", "", err
	}
	return access, refresh, refreshJTI, nil
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

// toClaimRoles конвертирует строки repo в claim-роли.
func toClaimRoles(rows []RoleRow) []ApartmentRole {
	out := make([]ApartmentRole, 0, len(rows))
	for _, r := range rows {
		out = append(out, ApartmentRole{
			ApartmentID:     r.ApartmentID,
			Role:            r.Role,
			CanCreateGuests: r.CanCreateGuests,
		})
	}
	return out
}

// profile строит UserProfile из готовых claim-ролей.
func profile(id string, kind Kind, roles []ApartmentRole, mcID string) UserProfile {
	apts := make([]ApartmentInfo, 0, len(roles))
	for _, r := range roles {
		apts = append(apts, ApartmentInfo{ID: r.ApartmentID, Role: r.Role})
	}
	return UserProfile{ID: id, Kind: kind, Apartments: apts, MCID: mcID}
}

// profileFromClaims строит UserProfile из claim-ов (для /me).
func profileFromClaims(c Claims) UserProfile {
	return profile(c.Subject, c.Kind, c.Roles, c.MCID)
}
