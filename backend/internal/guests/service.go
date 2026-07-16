package guests

// Прикладная логика гостевого доступа. RBAC: создавать гостей вправе владелец или
// жилец с can_create_guests (проверка по БД); точки гостя ⊆ доступных создателю;
// на открытии доступ переспрашивается (производность). Открытие делегируется
// access-машинерии через DoorOpener. Секреты в аудит/логи не попадают.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// OpenResult — результат открытия точки (зеркалит access.OpenResult).
type OpenResult struct {
	RequestID string
	Status    string
}

// DoorOpener исполняет открытие уже РАЗРЕШЁННОЙ точки (presence→MQTT→аудит).
// Реализация — адаптер поверх access.Service.OpenResolved (граница модуля).
type DoorOpener interface {
	OpenResolved(ctx context.Context, rp ResolvedPoint, actor string, meta map[string]any) (OpenResult, *httpx.Error)
}

// PresenceChecker сообщает online-статус устройства (для страницы гостя).
// Боевая реализация — devices.Presence (структурная типизация).
type PresenceChecker interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
}

// Service — доменная логика гостевого доступа.
type Service struct {
	repo     *Repo
	opener   DoorOpener
	presence PresenceChecker
	baseURL  string // база гостевой ссылки (VISITOR_BASE_URL, без хвостового /)
	audit    audit.Recorder
	log      *slog.Logger
	now      func() time.Time
}

// NewService собирает сервис гостей.
func NewService(repo *Repo, opener DoorOpener, presence PresenceChecker, baseURL string, recorder audit.Recorder, log *slog.Logger) *Service {
	return &Service{
		repo:     repo,
		opener:   opener,
		presence: presence,
		baseURL:  strings.TrimRight(baseURL, "/"),
		audit:    recorder,
		log:      log,
		now:      time.Now,
	}
}

// CreateResult — выданная гостевая ссылка (мок доставки: создатель шлёт сам).
type CreateResult struct {
	GuestID   string
	Token     string
	URL       string
	ValidFrom time.Time
	ValidTo   time.Time
}

// PointOptionView / GuestView — данные для формы и страницы гостя.
type PointView struct {
	PublicID string
	Label    string
	Type     string
	Online   bool
}

type GuestView struct {
	FullName  string
	ValidFrom time.Time
	ValidTo   time.Time
	Points    []PointView
}

// GuestPointOptions возвращает точки, которые создатель вправе дать гостю квартиры
// (для формы). Требует членства вызывающего в квартире.
func (s *Service) GuestPointOptions(ctx context.Context, claims auth.Claims, apartmentID string) ([]PointOption, *httpx.Error) {
	member, err := s.repo.IsApartmentMember(ctx, claims.Subject, apartmentID)
	if err != nil {
		return nil, s.internal("is_member_failed", err)
	}
	if !member {
		return nil, httpx.NewError(httpx.CodeForbidden, "You are not a member of this apartment")
	}
	opts, err := s.repo.AllowedPoints(ctx, apartmentID, claims.Subject)
	if err != nil {
		return nil, s.internal("allowed_points_failed", err)
	}
	return opts, nil
}

// CreateGuest выдаёт гостевой доступ: право (owner|can_create_guests), точки ⊆
// доступных создателю, окно ≤ 2 дней. valid_from = now.
func (s *Service) CreateGuest(ctx context.Context, claims auth.Claims, apartmentID, fullName string, validTo time.Time, pointPublicIDs []string) (CreateResult, *httpx.Error) {
	mc, ok, err := s.repo.ApartmentMC(ctx, apartmentID)
	if err != nil {
		return CreateResult{}, s.internal("apartment_mc_failed", err)
	}
	if !ok {
		return CreateResult{}, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	allowed, err := s.repo.CanCreateGuests(ctx, claims.Subject, apartmentID)
	if err != nil {
		return CreateResult{}, s.internal("can_create_guests_failed", err)
	}
	if !allowed {
		return CreateResult{}, httpx.NewError(httpx.CodeForbidden, "You may not create guests for this apartment")
	}

	from := s.now()
	if apiErr := ValidateWindow(from, validTo); apiErr != nil {
		return CreateResult{}, apiErr
	}

	// Набор точек ⊆ доступных создателю (иначе выдал бы доступ, которого у него нет).
	opts, err := s.repo.AllowedPoints(ctx, apartmentID, claims.Subject)
	if err != nil {
		return CreateResult{}, s.internal("allowed_points_failed", err)
	}
	allow := make(map[string]string, len(opts)) // publicID → internal id
	for _, o := range opts {
		allow[o.PublicID] = o.ID
	}
	seen := map[string]struct{}{}
	pointIDs := make([]string, 0, len(pointPublicIDs))
	for _, pid := range pointPublicIDs {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			continue
		}
		if _, dup := seen[pid]; dup {
			continue
		}
		seen[pid] = struct{}{}
		internalID, okp := allow[pid]
		if !okp {
			return CreateResult{}, httpx.NewError(httpx.CodeForbidden, "Access point is not available to you")
		}
		pointIDs = append(pointIDs, internalID)
	}
	if len(pointIDs) == 0 {
		return CreateResult{}, httpx.NewError(httpx.CodeValidationError, "At least one access point is required")
	}

	raw, err := GenerateToken()
	if err != nil {
		return CreateResult{}, s.internal("generate_token_failed", err)
	}
	guestID, err := s.repo.CreateGuest(ctx, NewGuest{
		TokenHash:   HashToken(raw),
		FullName:    strings.TrimSpace(fullName),
		ApartmentID: apartmentID,
		MCID:        mc,
		CreatedBy:   claims.Subject,
		ValidFrom:   from,
		ValidTo:     validTo,
		PointIDs:    pointIDs,
	})
	if err != nil {
		return CreateResult{}, s.internal("create_guest_failed", err)
	}

	s.record(ctx, audit.Event{
		EventType:           "guest_created",
		Actor:               "user:" + claims.Subject,
		ApartmentID:         apartmentID,
		ManagementCompanyID: mc,
		Metadata:            map[string]any{"guest_id": guestID, "points": len(pointIDs), "valid_to": validTo.UTC().Format(time.RFC3339)},
	})

	return CreateResult{
		GuestID:   guestID,
		Token:     raw,
		URL:       s.baseURL + "/g/" + raw,
		ValidFrom: from,
		ValidTo:   validTo,
	}, nil
}

// ViewGuest отдаёт данные страницы /g/{token}: имя, окно, точки со статусом.
// Невалидный токен → GUEST_INVALID; вне окна/отозван → GUEST_EXPIRED.
func (s *Service) ViewGuest(ctx context.Context, rawToken string) (GuestView, *httpx.Error) {
	g, ok, err := s.repo.GetGuestByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return GuestView{}, s.internal("get_guest_failed", err)
	}
	if !ok {
		return GuestView{}, httpx.NewError(httpx.CodeGuestInvalid, "Guest link not found")
	}
	if apiErr := (Guest{ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, RevokedAt: g.RevokedAt}).Validate(s.now()); apiErr != nil {
		return GuestView{}, apiErr
	}
	pts, err := s.repo.GuestPoints(ctx, g.ID)
	if err != nil {
		return GuestView{}, s.internal("guest_points_failed", err)
	}
	views := make([]PointView, 0, len(pts))
	for _, p := range pts {
		online, err := s.presence.IsOnline(ctx, p.DeviceID)
		if err != nil {
			s.log.Warn("presence_check_failed", "error", err, "device_id", p.DeviceID)
			online = false
		}
		views = append(views, PointView{PublicID: p.PublicID, Label: p.Label, Type: p.Type, Online: online})
	}
	return GuestView{FullName: g.FullName, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Points: views}, nil
}

// OpenAsGuest открывает точку по гостевой ссылке: валидность окна + производный
// доступ создателя (переспрос) → делегирование в DoorOpener.
func (s *Service) OpenAsGuest(ctx context.Context, rawToken, publicID string) (OpenResult, *httpx.Error) {
	g, ok, err := s.repo.GetGuestByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return OpenResult{}, s.internal("get_guest_failed", err)
	}
	if !ok {
		return OpenResult{}, httpx.NewError(httpx.CodeGuestInvalid, "Guest link not found")
	}
	if apiErr := (Guest{ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, RevokedAt: g.RevokedAt}).Validate(s.now()); apiErr != nil {
		return OpenResult{}, apiErr
	}
	rp, ok, err := s.repo.ResolveGuestOpenPoint(ctx, g.ID, g.CreatedBy, g.ApartmentID, publicID)
	if err != nil {
		return OpenResult{}, s.internal("resolve_guest_point_failed", err)
	}
	if !ok {
		s.log.Warn("guest_open_forbidden", "guest_id", g.ID, "public_id", publicID)
		return OpenResult{}, httpx.NewError(httpx.CodeForbidden, "This point is not available")
	}
	return s.opener.OpenResolved(ctx, rp, "guest:"+g.ID, map[string]any{"guest_id": g.ID})
}

// ListGuests возвращает гостей, созданных вызывающим (без токенов).
func (s *Service) ListGuests(ctx context.Context, claims auth.Claims) ([]GuestSummary, *httpx.Error) {
	out, err := s.repo.ListByCreator(ctx, claims.Subject)
	if err != nil {
		return nil, s.internal("list_guests_failed", err)
	}
	return out, nil
}

// Revoke отзывает гостя (создатель или владелец квартиры). Не найден в скоупе →
// GUEST_INVALID.
func (s *Service) Revoke(ctx context.Context, claims auth.Claims, guestID string) *httpx.Error {
	ok, err := s.repo.Revoke(ctx, guestID, claims.Subject, s.now())
	if err != nil {
		return s.internal("revoke_failed", err)
	}
	if !ok {
		return httpx.NewError(httpx.CodeGuestInvalid, "Guest not found")
	}
	s.record(ctx, audit.Event{
		EventType: "guest_revoked",
		Actor:     "user:" + claims.Subject,
		Metadata:  map[string]any{"guest_id": guestID},
	})
	return nil
}

// SetPermission делегирует can_create_guests жильцу квартиры (вправе только
// владелец этой квартиры).
func (s *Service) SetPermission(ctx context.Context, claims auth.Claims, apartmentID, targetUserID string, enabled bool) *httpx.Error {
	owns, err := s.repo.OwnsApartment(ctx, claims.Subject, apartmentID)
	if err != nil {
		return s.internal("owns_apartment_failed", err)
	}
	if !owns {
		return httpx.NewError(httpx.CodeForbidden, "You are not the owner of this apartment")
	}
	ok, err := s.repo.SetGuestPermission(ctx, apartmentID, targetUserID, enabled)
	if err != nil {
		return s.internal("set_permission_failed", err)
	}
	if !ok {
		return httpx.NewError(httpx.CodeValidationError, "User is not a resident of this apartment")
	}
	s.record(ctx, audit.Event{
		EventType:   "guest_right_changed",
		Actor:       "user:" + claims.Subject,
		ApartmentID: apartmentID,
		Metadata:    map[string]any{"target": targetUserID, "enabled": enabled},
	})
	return nil
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
