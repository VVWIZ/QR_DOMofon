package guests

// pgx-репозиторий гостевого доступа. Только параметризованный SQL. Секрет ссылки
// в БД не хранится — приходит уже как SHA-256-хеш. Производный доступ («какие
// точки создатель вправе дать» и «есть ли у него доступ сейчас») выражен одним
// предикатом-источником истины, переиспользуемым при создании, показе и открытии.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo — доступ к guest_access/guest_access_points (+ смежные apartments/roles/grants).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий гостей.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// NewGuest — параметры создаваемого гостя (PointIDs — внутренние access_points.id,
// уже провалидированы как разрешённые создателю).
type NewGuest struct {
	TokenHash   string
	FullName    string
	ApartmentID string
	MCID        string
	CreatedBy   string
	ValidFrom   time.Time
	ValidTo     time.Time
	PointIDs    []string
}

// GuestRow — строка guest_access для проверки окна и резолва открытия.
type GuestRow struct {
	ID          string
	FullName    string
	ApartmentID string
	MCID        string
	CreatedBy   string
	ValidFrom   time.Time
	ValidTo     time.Time
	RevokedAt   *time.Time
}

// PointOption — точка, доступная создателю для выдачи гостю (форма создания).
type PointOption struct {
	ID       string // внутренний access_points.id (для guest_access_points)
	PublicID string
	Label    string
	Type     string
}

// GuestPointView — точка гостя для страницы /g/{token} (набор + устройство).
type GuestPointView struct {
	PublicID string
	Label    string
	Type     string
	DeviceID string
}

// ResolvedPoint — разрешённая на открытии точка (для передачи в DoorOpener).
type ResolvedPoint struct {
	DeviceID            string
	AccessPointID       string
	ApartmentID         string
	ManagementCompanyID string
}

// GuestSummary — краткая запись гостя для листинга создателем (без токена!).
type GuestSummary struct {
	ID        string
	FullName  string
	ValidFrom time.Time
	ValidTo   time.Time
	RevokedAt *time.Time
	Points    int
}

// allowedPointsCTE — предикат-источник истины «точки, которые создатель вправе
// дать гостю квартиры»: подъездные точки дома квартиры (по членству создателя) +
// калитки/шлагбаумы, на которые у создателя есть грант. $1=apartmentID,
// $2=createdBy. Используется в AllowedPoints и (той же логикой) в резолве открытия.
const allowedPointsCTE = `
	SELECT ap.id, ap.public_id, ap.label, ap.type
	FROM apartments a
	JOIN access_points ap
	    ON ap.building_id = a.building_id AND ap.type = 'entrance' AND ap.is_active = true
	   AND (a.entrance_id IS NULL OR ap.entrance_id IS NULL OR ap.entrance_id = a.entrance_id)
	WHERE a.id = $1 AND a.is_active = true
	  AND EXISTS (SELECT 1 FROM user_apartment_roles r WHERE r.user_id = $2 AND r.apartment_id = a.id)
	UNION
	SELECT ap.id, ap.public_id, ap.label, ap.type
	FROM user_access_grants ug
	JOIN access_points ap ON ap.id = ug.access_point_id AND ap.is_active = true
	WHERE ug.user_id = $2 AND ap.type IN ('gate', 'barrier')`

// ApartmentMC возвращает mc активной квартиры (для скоупа/записи гостя). ok=false
// → не-UUID/нет/неактивна.
func (r *Repo) ApartmentMC(ctx context.Context, apartmentID string) (string, bool, error) {
	if _, err := uuid.Parse(apartmentID); err != nil {
		return "", false, nil
	}
	var mc string
	err := r.pool.QueryRow(ctx,
		`SELECT management_company_id::text FROM apartments WHERE id = $1 AND is_active = true`,
		apartmentID,
	).Scan(&mc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("guests: apartment mc: %w", err)
	}
	return mc, true, nil
}

// CanCreateGuests сообщает, вправе ли userID создавать гостей в квартире
// apartmentID: роль owner ИЛИ can_create_guests=true. Проверяется по БД (не по
// claims): свежее делегирование действует сразу, отозванное — сразу перестаёт.
func (r *Repo) CanCreateGuests(ctx context.Context, userID, apartmentID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_apartment_roles
			WHERE user_id = $1 AND apartment_id = $2
			  AND (role = 'owner' OR can_create_guests = true)
		)`, userID, apartmentID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("guests: can create guests: %w", err)
	}
	return ok, nil
}

// IsApartmentMember сообщает, есть ли у userID роль в квартире (для GET
// guest-points: показать точки только своей квартиры).
func (r *Repo) IsApartmentMember(ctx context.Context, userID, apartmentID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM user_apartment_roles WHERE user_id = $1 AND apartment_id = $2)`,
		userID, apartmentID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("guests: is apartment member: %w", err)
	}
	return ok, nil
}

// AllowedPoints — точки, которые создатель вправе дать гостю квартиры (форма +
// валидация набора при создании).
func (r *Repo) AllowedPoints(ctx context.Context, apartmentID, createdBy string) ([]PointOption, error) {
	rows, err := r.pool.Query(ctx, allowedPointsCTE+" ORDER BY 4, 3", apartmentID, createdBy)
	if err != nil {
		return nil, fmt.Errorf("guests: allowed points: %w", err)
	}
	defer rows.Close()
	var out []PointOption
	for rows.Next() {
		var p PointOption
		if err := rows.Scan(&p.ID, &p.PublicID, &p.Label, &p.Type); err != nil {
			return nil, fmt.Errorf("guests: scan allowed point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateGuest вставляет гостя и его набор точек в одной транзакции.
func (r *Repo) CreateGuest(ctx context.Context, in NewGuest) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("guests: create begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	guestID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO guest_access
			(id, token_hash, full_name, apartment_id, management_company_id,
			 created_by, valid_from, valid_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		guestID, in.TokenHash, in.FullName, in.ApartmentID, in.MCID,
		in.CreatedBy, in.ValidFrom, in.ValidTo,
	); err != nil {
		return "", fmt.Errorf("guests: insert guest: %w", err)
	}
	for _, pid := range in.PointIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO guest_access_points (guest_access_id, access_point_id, management_company_id)
			VALUES ($1, $2, $3) ON CONFLICT (guest_access_id, access_point_id) DO NOTHING`,
			guestID, pid, in.MCID,
		); err != nil {
			return "", fmt.Errorf("guests: insert guest point: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("guests: create commit: %w", err)
	}
	return guestID, nil
}

// GetGuestByTokenHash возвращает гостя по хешу токена. ok=false → нет такого
// (наружу GUEST_INVALID 404, без различения нет/чужой).
func (r *Repo) GetGuestByTokenHash(ctx context.Context, tokenHash string) (GuestRow, bool, error) {
	const q = `
		SELECT id::text, full_name, apartment_id::text, management_company_id::text,
		       created_by::text, valid_from, valid_to, revoked_at
		FROM guest_access WHERE token_hash = $1`
	var g GuestRow
	err := r.pool.QueryRow(ctx, q, tokenHash).Scan(
		&g.ID, &g.FullName, &g.ApartmentID, &g.MCID, &g.CreatedBy,
		&g.ValidFrom, &g.ValidTo, &g.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return GuestRow{}, false, nil
	}
	if err != nil {
		return GuestRow{}, false, fmt.Errorf("guests: get by token: %w", err)
	}
	return g, true, nil
}

// GuestPoints возвращает набор точек гостя (для страницы /g/{token}); online
// домешивает сервис по DeviceID.
func (r *Repo) GuestPoints(ctx context.Context, guestID string) ([]GuestPointView, error) {
	const q = `
		SELECT ap.public_id::text, ap.label, ap.type, d.id::text
		FROM guest_access_points gap
		JOIN access_points ap ON ap.id = gap.access_point_id AND ap.is_active = true
		JOIN devices d ON d.access_point_id = ap.id
		WHERE gap.guest_access_id = $1
		ORDER BY ap.type, ap.label`
	rows, err := r.pool.Query(ctx, q, guestID)
	if err != nil {
		return nil, fmt.Errorf("guests: guest points: %w", err)
	}
	defer rows.Close()
	var out []GuestPointView
	for rows.Next() {
		var p GuestPointView
		if err := rows.Scan(&p.PublicID, &p.Label, &p.Type, &p.DeviceID); err != nil {
			return nil, fmt.Errorf("guests: scan guest point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ResolveGuestOpenPoint — производный резолв на ОТКРЫТИИ: точка входит в набор
// гостя И создатель ВСЁ ЕЩЁ вправе её открыть (подъезд — по членству в квартире;
// gate/barrier — по действующему гранту). ok=false → точки нет в наборе ИЛИ
// доступ создателя утрачен (наружу FORBIDDEN). Это тот же предикат, что
// AllowedPoints, но пересечённый с набором гостя и конкретной точкой.
func (r *Repo) ResolveGuestOpenPoint(ctx context.Context, guestID, createdBy, apartmentID, publicID string) (ResolvedPoint, bool, error) {
	if _, err := uuid.Parse(publicID); err != nil {
		return ResolvedPoint{}, false, nil
	}
	const q = `
		SELECT d.id::text, ap.id::text, a.id::text, ap.management_company_id::text
		FROM guest_access_points gap
		JOIN access_points ap ON ap.id = gap.access_point_id AND ap.is_active = true AND ap.public_id = $4
		JOIN devices d ON d.access_point_id = ap.id
		JOIN apartments a ON a.id = $3 AND a.is_active = true
		WHERE gap.guest_access_id = $1
		  AND (
		    ( ap.type = 'entrance'
		      AND ap.building_id = a.building_id
		      AND (a.entrance_id IS NULL OR ap.entrance_id IS NULL OR ap.entrance_id = a.entrance_id)
		      AND EXISTS (SELECT 1 FROM user_apartment_roles r WHERE r.user_id = $2 AND r.apartment_id = a.id)
		    )
		    OR
		    ( ap.type IN ('gate', 'barrier')
		      AND EXISTS (SELECT 1 FROM user_access_grants ug WHERE ug.user_id = $2 AND ug.access_point_id = ap.id)
		    )
		  )`
	var rp ResolvedPoint
	err := r.pool.QueryRow(ctx, q, guestID, createdBy, apartmentID, publicID).
		Scan(&rp.DeviceID, &rp.AccessPointID, &rp.ApartmentID, &rp.ManagementCompanyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedPoint{}, false, nil
	}
	if err != nil {
		return ResolvedPoint{}, false, fmt.Errorf("guests: resolve open point: %w", err)
	}
	return rp, true, nil
}

// ListByCreator возвращает гостей, созданных createdBy (без токенов).
func (r *Repo) ListByCreator(ctx context.Context, createdBy string) ([]GuestSummary, error) {
	const q = `
		SELECT g.id::text, g.full_name, g.valid_from, g.valid_to, g.revoked_at,
		       (SELECT count(*) FROM guest_access_points gap WHERE gap.guest_access_id = g.id)
		FROM guest_access g
		WHERE g.created_by = $1
		ORDER BY g.created_at DESC`
	rows, err := r.pool.Query(ctx, q, createdBy)
	if err != nil {
		return nil, fmt.Errorf("guests: list by creator: %w", err)
	}
	defer rows.Close()
	var out []GuestSummary
	for rows.Next() {
		var s GuestSummary
		if err := rows.Scan(&s.ID, &s.FullName, &s.ValidFrom, &s.ValidTo, &s.RevokedAt, &s.Points); err != nil {
			return nil, fmt.Errorf("guests: scan summary: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Revoke помечает гостя отозванным. Право: создатель гостя ИЛИ владелец квартиры
// гостя. rows=0 → нет такого гостя в скоупе вызывающего.
func (r *Repo) Revoke(ctx context.Context, guestID, byUser string, now time.Time) (bool, error) {
	const q = `
		UPDATE guest_access g
		SET revoked_at = $3, revoked_by = $2
		WHERE g.id = $1 AND g.revoked_at IS NULL
		  AND (
		    g.created_by = $2
		    OR EXISTS (SELECT 1 FROM user_apartment_roles r
		               WHERE r.user_id = $2 AND r.apartment_id = g.apartment_id AND r.role = 'owner')
		  )`
	tag, err := r.pool.Exec(ctx, q, guestID, byUser, now)
	if err != nil {
		return false, fmt.Errorf("guests: revoke: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SetGuestPermission ставит can_create_guests жильцу targetUserID в квартире
// apartmentID (делегирование). rows=0 → target не привязан к этой квартире.
func (r *Repo) SetGuestPermission(ctx context.Context, apartmentID, targetUserID string, enabled bool) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE user_apartment_roles SET can_create_guests = $3
		 WHERE user_id = $1 AND apartment_id = $2`,
		targetUserID, apartmentID, enabled)
	if err != nil {
		return false, fmt.Errorf("guests: set permission: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// OwnsApartment сообщает, есть ли у userID роль owner в квартире (для
// делегирования: наделять правом вправе только владелец квартиры).
func (r *Repo) OwnsApartment(ctx context.Context, userID, apartmentID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM user_apartment_roles WHERE user_id = $1 AND apartment_id = $2 AND role = 'owner')`,
		userID, apartmentID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("guests: owns apartment: %w", err)
	}
	return ok, nil
}
