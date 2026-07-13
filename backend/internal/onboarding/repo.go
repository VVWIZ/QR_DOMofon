package onboarding

// pgx-репозиторий онбординга: создание инвайтов, атомарный приём (find-or-create
// пользователя + привязка + пометка used в одной транзакции), выдача постоянных
// грантов и выборки для УК. Только параметризованный SQL. Секрет ссылки в БД не
// хранится — приходит уже как SHA-256-хеш (token_hash).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"domofon/backend/internal/access"
	"domofon/backend/internal/platform/httpx"
)

// Repo — доступ к invites/user_access_grants (+ смежные users/roles/points).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий онбординга.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// NewInvite — параметры создаваемого инвайта (форма зависит от TargetKind,
// см. CHECK invites_shape в миграции 0005).
type NewInvite struct {
	TokenHash     string
	Phone         string
	TargetKind    string // apartment_owner | apartment_resident | access_grant
	ApartmentID   string // для apartment_*; "" для access_grant
	AccessPointID string // для access_grant; "" иначе
	Role          string // owner|resident для apartment_*; "" для access_grant
	MCID          string
	CreatedBy     string
	ExpiresAt     time.Time
}

// AccessPointRow — минимум по точке доступа для проверки скоупа/типа.
type AccessPointRow struct {
	ID    string
	MCID  string
	Type  string
	Label string
}

// ApartmentRow — минимум по квартире для проверки принадлежности УК.
type ApartmentRow struct {
	ID   string
	MCID string
}

// ResidentApartment — квартира жильца/владельца в выборке УК.
type ResidentApartment struct {
	ID     string
	Number string
	Role   string
}

// ResidentGrant — грант доступа пользователя в выборке УК.
type ResidentGrant struct {
	PublicID string
	Label    string
}

// Resident — агрегированная запись жильца/владельца УК.
type Resident struct {
	UserID     string
	Phone      string
	Kind       string
	Apartments []ResidentApartment
	Grants     []ResidentGrant
}

// CreateInvite вставляет новый инвайт. Nullable-колонки (phone/apartment_id/
// access_point_id/role) кладутся как NULL при пустом значении.
func (r *Repo) CreateInvite(ctx context.Context, in NewInvite) error {
	const q = `
		INSERT INTO invites
			(id, token_hash, phone, target_kind, apartment_id, access_point_id,
			 role, management_company_id, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.pool.Exec(ctx, q,
		uuid.NewString(), in.TokenHash, nullStr(in.Phone), in.TargetKind,
		nullStr(in.ApartmentID), nullStr(in.AccessPointID), nullStr(in.Role),
		in.MCID, in.CreatedBy, in.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("onboarding: create invite: %w", err)
	}
	return nil
}

// GetAccessPoint возвращает активную точку доступа по public_id. Не-UUID/не
// найдена → ok=false (наружу — VALIDATION_ERROR, не 500).
func (r *Repo) GetAccessPoint(ctx context.Context, publicID string) (AccessPointRow, bool, error) {
	if _, err := uuid.Parse(publicID); err != nil {
		return AccessPointRow{}, false, nil
	}
	const q = `
		SELECT id::text, management_company_id::text, type, label
		FROM access_points
		WHERE public_id = $1 AND is_active = true`
	var ap AccessPointRow
	err := r.pool.QueryRow(ctx, q, publicID).Scan(&ap.ID, &ap.MCID, &ap.Type, &ap.Label)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessPointRow{}, false, nil
	}
	if err != nil {
		return AccessPointRow{}, false, fmt.Errorf("onboarding: get access point: %w", err)
	}
	return ap, true, nil
}

// GetApartment возвращает активную квартиру по id. Не-UUID/не найдена → ok=false.
func (r *Repo) GetApartment(ctx context.Context, apartmentID string) (ApartmentRow, bool, error) {
	if _, err := uuid.Parse(apartmentID); err != nil {
		return ApartmentRow{}, false, nil
	}
	const q = `
		SELECT id::text, management_company_id::text
		FROM apartments
		WHERE id = $1 AND is_active = true`
	var a ApartmentRow
	err := r.pool.QueryRow(ctx, q, apartmentID).Scan(&a.ID, &a.MCID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApartmentRow{}, false, nil
	}
	if err != nil {
		return ApartmentRow{}, false, fmt.Errorf("onboarding: get apartment: %w", err)
	}
	return a, true, nil
}

// FindUserByPhoneInMC ищет пользователя по телефону, уже связанного с УК mcID
// (есть роль в квартире этой УК ИЛИ грант точки этой УК). ok=false → такого нет
// (нужен инвайт). Гарантирует, что грант не выдаётся «чужому» пользователю без
// подтверждения через инвайт.
func (r *Repo) FindUserByPhoneInMC(ctx context.Context, phone, mcID string) (string, bool, error) {
	const q = `
		SELECT u.id::text
		FROM users u
		WHERE u.phone = $1 AND (
			EXISTS (SELECT 1 FROM user_apartment_roles r
			        WHERE r.user_id = u.id AND r.management_company_id = $2)
			OR EXISTS (SELECT 1 FROM user_access_grants g
			           WHERE g.user_id = u.id AND g.management_company_id = $2)
		)
		LIMIT 1`
	var id string
	err := r.pool.QueryRow(ctx, q, phone, mcID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("onboarding: find user by phone in mc: %w", err)
	}
	return id, true, nil
}

// AttachAccessGrant выдаёт пользователю постоянный грант на точку (идемпотентно
// по UNIQUE(user_id, access_point_id)).
func (r *Repo) AttachAccessGrant(ctx context.Context, userID, accessPointID, mcID, grantedBy string) error {
	const q = `
		INSERT INTO user_access_grants
			(id, user_id, access_point_id, management_company_id, granted_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, access_point_id) DO NOTHING`
	_, err := r.pool.Exec(ctx, q, uuid.NewString(), userID, accessPointID, mcID, grantedBy)
	if err != nil {
		return fmt.Errorf("onboarding: attach access grant: %w", err)
	}
	return nil
}

// AcceptInviteTx атомарно принимает инвайт по token_hash: блокирует строку
// (FOR UPDATE), проверяет валидность (used/expiry), находит-или-создаёт
// пользователя по телефону, привязывает роль (квартира) или грант (точка) и
// помечает инвайт used. Пометка used атомарна (WHERE used_at IS NULL): проигрыш
// гонки → INVITE_INVALID. Возвращает id пользователя для выдачи токенов.
func (r *Repo) AcceptInviteTx(ctx context.Context, tokenHash string, now time.Time) (string, *httpx.Error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		inviteID, targetKind, mcID, createdBy   string
		phone, apartmentID, accessPointID, role *string
		usedAt                                  *time.Time
		expiresAt                               time.Time
	)
	const sel = `
		SELECT id::text, phone, target_kind, apartment_id::text, access_point_id::text,
		       role, management_company_id::text, created_by::text, used_at, expires_at
		FROM invites
		WHERE token_hash = $1
		FOR UPDATE`
	err = tx.QueryRow(ctx, sel, tokenHash).Scan(
		&inviteID, &phone, &targetKind, &apartmentID, &accessPointID,
		&role, &mcID, &createdBy, &usedAt, &expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.NewError(httpx.CodeInviteInvalid, "Invite not found")
	}
	if err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}

	// Валидность (used → expiry) — чистой логикой Invite.Validate.
	inv := Invite{TokenHash: tokenHash, UsedAt: usedAt, ExpiresAt: expiresAt}
	if apiErr := inv.Validate(now); apiErr != nil {
		return "", apiErr
	}
	if phone == nil || *phone == "" {
		return "", httpx.NewError(httpx.CodeInviteInvalid, "Invite has no target phone")
	}

	// find-or-create пользователя по телефону. Новый kind: owner для
	// apartment_owner, иначе resident (существующему kind не меняем).
	newKind := "resident"
	if targetKind == "apartment_owner" {
		newKind = "owner"
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (id, phone, kind) VALUES ($1, $2, $3)
		 ON CONFLICT (phone) DO NOTHING`,
		uuid.NewString(), *phone, newKind,
	); err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	var userID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE phone = $1`, *phone).Scan(&userID); err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}

	// Привязка по типу инвайта (идемпотентно).
	switch targetKind {
	case "apartment_owner", "apartment_resident":
		if apartmentID == nil || role == nil {
			return "", httpx.NewError(httpx.CodeInternal, "Malformed invite")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_apartment_roles
				(id, user_id, apartment_id, management_company_id, role, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (user_id, apartment_id) DO NOTHING`,
			uuid.NewString(), userID, *apartmentID, mcID, *role, createdBy,
		); err != nil {
			return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
		}
	case "access_grant":
		if accessPointID == nil {
			return "", httpx.NewError(httpx.CodeInternal, "Malformed invite")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_access_grants
				(id, user_id, access_point_id, management_company_id, granted_by)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (user_id, access_point_id) DO NOTHING`,
			uuid.NewString(), userID, *accessPointID, mcID, createdBy,
		); err != nil {
			return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
		}
	default:
		return "", httpx.NewError(httpx.CodeInternal, "Unknown invite target")
	}

	// Атомарная пометка used: проигрыш гонки (0 строк) → инвайт уже использован.
	tag, err := tx.Exec(ctx,
		`UPDATE invites SET used_at = $1, used_by = $2 WHERE id = $3 AND used_at IS NULL`,
		now, userID, inviteID,
	)
	if err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	if tag.RowsAffected() == 0 {
		return "", httpx.NewError(httpx.CodeInviteInvalid, "Invite has already been used")
	}

	if err := tx.Commit(ctx); err != nil {
		return "", httpx.NewError(httpx.CodeInternal, "Internal server error")
	}
	return userID, nil
}

// ResolveGrantedPoint реализует access.PointResolver: активный грant пользователя
// на точку по её public_id + устройство точки. ok=false → гранта нет (→ 403).
func (r *Repo) ResolveGrantedPoint(ctx context.Context, userID, publicID string) (access.GrantedPoint, bool, error) {
	if _, err := uuid.Parse(publicID); err != nil {
		return access.GrantedPoint{}, false, nil
	}
	const q = `
		SELECT d.id::text, ap.id::text, ap.management_company_id::text
		FROM user_access_grants g
		JOIN access_points ap ON ap.id = g.access_point_id AND ap.is_active = true
		JOIN devices d ON d.access_point_id = ap.id
		WHERE g.user_id = $1 AND ap.public_id = $2`
	var gp access.GrantedPoint
	err := r.pool.QueryRow(ctx, q, userID, publicID).Scan(&gp.DeviceID, &gp.AccessPointID, &gp.ManagementCompanyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return access.GrantedPoint{}, false, nil
	}
	if err != nil {
		return access.GrantedPoint{}, false, fmt.Errorf("onboarding: resolve granted point: %w", err)
	}
	return gp, true, nil
}

// ListGrantedPoints реализует access.PointLister: все точки, на которые у
// пользователя есть грант (для /access/points).
func (r *Repo) ListGrantedPoints(ctx context.Context, userID string) ([]access.GrantedPointInfo, error) {
	const q = `
		SELECT ap.public_id::text, ap.label, ap.type, d.id::text
		FROM user_access_grants g
		JOIN access_points ap ON ap.id = g.access_point_id AND ap.is_active = true
		JOIN devices d ON d.access_point_id = ap.id
		WHERE g.user_id = $1
		ORDER BY ap.label`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: list granted points: %w", err)
	}
	defer rows.Close()

	var out []access.GrantedPointInfo
	for rows.Next() {
		var p access.GrantedPointInfo
		if err := rows.Scan(&p.PublicID, &p.Label, &p.Type, &p.DeviceID); err != nil {
			return nil, fmt.Errorf("onboarding: scan granted point: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("onboarding: rows granted points: %w", err)
	}
	return out, nil
}

// ListResidents возвращает всех жильцов/владельцев УК mcID (по ролям в квартирах
// и по грантам точек), агрегируя квартиры и гранты по пользователю.
func (r *Repo) ListResidents(ctx context.Context, mcID string) ([]Resident, error) {
	byID := map[string]*Resident{}
	get := func(id, phone, kind string) *Resident {
		res, ok := byID[id]
		if !ok {
			res = &Resident{UserID: id, Phone: phone, Kind: kind}
			byID[id] = res
		}
		return res
	}

	// Роли в квартирах УК.
	const qRoles = `
		SELECT u.id::text, COALESCE(u.phone, ''), u.kind, a.id::text, a.number, r.role
		FROM user_apartment_roles r
		JOIN users u ON u.id = r.user_id
		JOIN apartments a ON a.id = r.apartment_id
		WHERE r.management_company_id = $1
		ORDER BY u.phone, a.number`
	rows, err := r.pool.Query(ctx, qRoles, mcID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: list residents roles: %w", err)
	}
	for rows.Next() {
		var uid, phone, kind, aid, number, role string
		if err := rows.Scan(&uid, &phone, &kind, &aid, &number, &role); err != nil {
			rows.Close()
			return nil, fmt.Errorf("onboarding: scan resident role: %w", err)
		}
		res := get(uid, phone, kind)
		res.Apartments = append(res.Apartments, ResidentApartment{ID: aid, Number: number, Role: role})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("onboarding: rows resident roles: %w", err)
	}

	// Гранты точек УК.
	const qGrants = `
		SELECT u.id::text, COALESCE(u.phone, ''), u.kind, ap.public_id::text, ap.label
		FROM user_access_grants g
		JOIN users u ON u.id = g.user_id
		JOIN access_points ap ON ap.id = g.access_point_id
		WHERE g.management_company_id = $1
		ORDER BY u.phone, ap.label`
	grows, err := r.pool.Query(ctx, qGrants, mcID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: list residents grants: %w", err)
	}
	for grows.Next() {
		var uid, phone, kind, publicID, label string
		if err := grows.Scan(&uid, &phone, &kind, &publicID, &label); err != nil {
			grows.Close()
			return nil, fmt.Errorf("onboarding: scan resident grant: %w", err)
		}
		res := get(uid, phone, kind)
		res.Grants = append(res.Grants, ResidentGrant{PublicID: publicID, Label: label})
	}
	grows.Close()
	if err := grows.Err(); err != nil {
		return nil, fmt.Errorf("onboarding: rows resident grants: %w", err)
	}

	out := make([]Resident, 0, len(byID))
	for _, res := range byID {
		out = append(out, *res)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Phone != out[j].Phone {
			return out[i].Phone < out[j].Phone
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

// nullStr возвращает nil для пустой строки (→ SQL NULL), иначе саму строку.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
