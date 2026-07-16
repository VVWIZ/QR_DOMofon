// Package sysadmin — платформенная (наша) админка: создание УК, объектов (ЖК/
// отдельных объектов), домов, подъездов и УК-администраторов. Единственный
// писатель management_companies/sites/buildings/entrances. Доступ — только роль
// system_admin (не ограничен management_company_id). onboarding не импортирует.
package sysadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Сентинелы репозитория (сервис маппит в httpx-коды).
var (
	// ErrConflict — нарушено ограничение уникальности (имя/адрес уже заняты).
	ErrConflict = errors.New("sysadmin: already exists")
	// ErrParentNotFound — родитель (УК/объект/дом) не существует.
	ErrParentNotFound = errors.New("sysadmin: parent not found")
)

// Repo — доступ к таблицам иерархии поверх pgxpool.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// MCRow — УК со счётчиками (для списка).
type MCRow struct {
	ID        string
	Name      string
	Sites     int
	Buildings int
}

// SiteRow / BuildingRow / EntranceRow — узлы каталога УК.
type EntranceRow struct {
	ID     string
	Number string
}
type BuildingRow struct {
	ID        string
	Address   string
	Entrances []EntranceRow
}
type SiteRow struct {
	ID        string
	Name      string
	Address   string
	Kind      string
	Buildings []BuildingRow
}

// ListMCs возвращает все УК со счётчиками объектов и домов.
func (r *Repo) ListMCs(ctx context.Context) ([]MCRow, error) {
	const q = `
		SELECT mc.id::text, mc.name,
		       (SELECT count(*) FROM sites s WHERE s.management_company_id = mc.id),
		       (SELECT count(*) FROM buildings b WHERE b.management_company_id = mc.id)
		FROM management_companies mc
		ORDER BY mc.name`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("sysadmin: list mcs: %w", err)
	}
	defer rows.Close()
	var out []MCRow
	for rows.Next() {
		var m MCRow
		if err := rows.Scan(&m.ID, &m.Name, &m.Sites, &m.Buildings); err != nil {
			return nil, fmt.Errorf("sysadmin: scan mc: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMC создаёт УК. Дубль имени → ErrConflict.
func (r *Repo) CreateMC(ctx context.Context, name string) (string, error) {
	id := uuid.NewString()
	var got string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO management_companies (id, name) VALUES ($1, $2)
		 ON CONFLICT (name) DO NOTHING RETURNING id::text`, id, name).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("sysadmin: create mc: %w", err)
	}
	return got, nil
}

// CreateMCAdmin создаёт УК-администратора (kind=mc_admin) для существующей УК.
// Дубль email → ErrConflict; нет УК → ErrParentNotFound.
func (r *Repo) CreateMCAdmin(ctx context.Context, mcID, email, fullName, passwordHash, totpSecret string) (string, error) {
	if !r.exists(ctx, `SELECT 1 FROM management_companies WHERE id = $1`, mcID) {
		return "", ErrParentNotFound
	}
	id := uuid.NewString()
	var got string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, full_name, password_hash, totp_secret, kind, management_company_id)
		VALUES ($1, $2, $3, $4, $5, 'mc_admin', $6)
		ON CONFLICT (email) DO NOTHING RETURNING id::text`,
		id, email, nullStr(fullName), passwordHash, totpSecret, mcID).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("sysadmin: create mc admin: %w", err)
	}
	return got, nil
}

// CreateSite создаёт объект в УК. Нет УК → ErrParentNotFound; дубль имени в УК →
// ErrConflict.
func (r *Repo) CreateSite(ctx context.Context, mcID, name, address, kind string) (string, error) {
	if !r.exists(ctx, `SELECT 1 FROM management_companies WHERE id = $1`, mcID) {
		return "", ErrParentNotFound
	}
	id := uuid.NewString()
	var got string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sites (id, management_company_id, name, address, kind)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (management_company_id, name) DO NOTHING RETURNING id::text`,
		id, mcID, name, address, kind).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("sysadmin: create site: %w", err)
	}
	return got, nil
}

// CreateBuilding создаёт дом в объекте (mc берётся из объекта, не из тела —
// анти-спуфинг). Нет объекта → ErrParentNotFound; дубль адреса в объекте → ErrConflict.
func (r *Repo) CreateBuilding(ctx context.Context, siteID, address string) (string, error) {
	mc, ok, err := r.parentMC(ctx, `SELECT management_company_id::text FROM sites WHERE id = $1`, siteID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrParentNotFound
	}
	id := uuid.NewString()
	var got string
	err = r.pool.QueryRow(ctx, `
		INSERT INTO buildings (id, management_company_id, site_id, address)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (site_id, address) DO NOTHING RETURNING id::text`,
		id, mc, siteID, address).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("sysadmin: create building: %w", err)
	}
	return got, nil
}

// CreateEntrance создаёт подъезд в доме (mc из дома). Нет дома → ErrParentNotFound;
// дубль номера в доме → ErrConflict.
func (r *Repo) CreateEntrance(ctx context.Context, buildingID, number string) (string, error) {
	mc, ok, err := r.parentMC(ctx, `SELECT management_company_id::text FROM buildings WHERE id = $1`, buildingID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrParentNotFound
	}
	id := uuid.NewString()
	var got string
	err = r.pool.QueryRow(ctx, `
		INSERT INTO entrances (id, building_id, management_company_id, number)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (building_id, number) DO NOTHING RETURNING id::text`,
		id, buildingID, mc, number).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", fmt.Errorf("sysadmin: create entrance: %w", err)
	}
	return got, nil
}

// MoveBuilding перевешивает дом на другой объект ТОЙ ЖЕ УК (композитный FK гардит
// чужую УК). 0 строк → нет дома / объект другой УК → ErrParentNotFound.
func (r *Repo) MoveBuilding(ctx context.Context, buildingID, newSiteID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE buildings b SET site_id = $2
		WHERE b.id = $1
		  AND EXISTS (SELECT 1 FROM sites s WHERE s.id = $2 AND s.management_company_id = b.management_company_id)`,
		buildingID, newSiteID)
	if err != nil {
		return fmt.Errorf("sysadmin: move building: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrParentNotFound
	}
	return nil
}

// Catalog возвращает дерево объект→дом→подъезд выбранной УК (для /system UI).
func (r *Repo) Catalog(ctx context.Context, mcID string) ([]SiteRow, error) {
	const q = `
		SELECT s.id::text, s.name, s.address, s.kind,
		       COALESCE(b.id::text, ''), COALESCE(b.address, ''),
		       COALESCE(e.id::text, ''), COALESCE(e.number, '')
		FROM sites s
		LEFT JOIN buildings b ON b.site_id = s.id
		LEFT JOIN entrances e ON e.building_id = b.id
		WHERE s.management_company_id = $1
		ORDER BY s.name, b.address, e.number`
	rows, err := r.pool.Query(ctx, q, mcID)
	if err != nil {
		return nil, fmt.Errorf("sysadmin: catalog: %w", err)
	}
	defer rows.Close()

	var out []SiteRow
	sIdx := map[string]int{}
	bIdx := map[string]int{} // "siteID|buildingID" → индекс в Buildings
	for rows.Next() {
		var sid, sname, saddr, skind, bid, baddr, eid, enum string
		if err := rows.Scan(&sid, &sname, &saddr, &skind, &bid, &baddr, &eid, &enum); err != nil {
			return nil, fmt.Errorf("sysadmin: scan catalog: %w", err)
		}
		si, ok := sIdx[sid]
		if !ok {
			si = len(out)
			sIdx[sid] = si
			out = append(out, SiteRow{ID: sid, Name: sname, Address: saddr, Kind: skind})
		}
		if bid == "" {
			continue
		}
		bkey := sid + "|" + bid
		bi, ok := bIdx[bkey]
		if !ok {
			bi = len(out[si].Buildings)
			bIdx[bkey] = bi
			out[si].Buildings = append(out[si].Buildings, BuildingRow{ID: bid, Address: baddr})
		}
		if eid == "" {
			continue
		}
		bld := &out[si].Buildings[bi]
		bld.Entrances = append(bld.Entrances, EntranceRow{ID: eid, Number: enum})
	}
	return out, rows.Err()
}

// --- helpers ---

func (r *Repo) exists(ctx context.Context, q, arg string) bool {
	var one int
	err := r.pool.QueryRow(ctx, q, arg).Scan(&one)
	return err == nil
}

// parentMC возвращает mc родителя по запросу q(arg). ok=false → родителя нет.
func (r *Repo) parentMC(ctx context.Context, q, arg string) (string, bool, error) {
	var mc string
	err := r.pool.QueryRow(ctx, q, arg).Scan(&mc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("sysadmin: parent mc: %w", err)
	}
	return mc, true, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
