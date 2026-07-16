package schedules

// pgx-репозиторий расписаний: CRUD (скоуп по mc) + выборка активных окон для
// reconciler. Время окна хранится как time, наружу — "HH:MM".

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo — доступ к access_point_schedules.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Row — расписание точки (для списка УК).
type Row struct {
	ID       string
	Dow      int
	Opens    string
	Closes   string
	Timezone string
	IsActive bool
}

// PointSchedules — точка gate/barrier + её окна (для экрана «Калитки/Шлагбаумы»).
type PointSchedules struct {
	PublicID  string
	Label     string
	Type      string
	Schedules []Row
}

// DueRow — активное окно + устройство точки (для reconciler; фильтр по времени — в Go).
type DueRow struct {
	AccessPointID string
	DeviceID      string
	MCID          string
	Sched         Schedule
}

// ResolvePoint возвращает внутренний id точки gate/barrier по public_id в рамках
// УК. ok=false → нет/чужая/не gate-barrier.
func (r *Repo) ResolvePoint(ctx context.Context, publicID, mcID string) (string, bool, error) {
	if _, err := uuid.Parse(publicID); err != nil {
		return "", false, nil
	}
	var id, mc, typ string
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, management_company_id::text, type FROM access_points WHERE public_id=$1 AND is_active=true`,
		publicID).Scan(&id, &mc, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("schedules: resolve point: %w", err)
	}
	if mc != mcID || (typ != "gate" && typ != "barrier") {
		return "", false, nil
	}
	return id, true, nil
}

// Create вставляет окно (opens/closes — "HH:MM").
func (r *Repo) Create(ctx context.Context, apID, mcID string, s Schedule, createdBy string) (string, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO access_point_schedules
			(id, access_point_id, management_company_id, dow, opens, closes, timezone, created_by)
		VALUES ($1,$2,$3,$4,$5::time,$6::time,$7,$8)`,
		id, apID, mcID, s.Dow, s.Opens, s.Closes, s.Timezone, createdBy)
	if err != nil {
		return "", fmt.Errorf("schedules: create: %w", err)
	}
	return id, nil
}

// Delete удаляет окно (скоуп по mc). removed=false → нет такого в этой УК.
func (r *Repo) Delete(ctx context.Context, id, mcID string) (bool, error) {
	if _, err := uuid.Parse(id); err != nil {
		return false, nil
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM access_point_schedules WHERE id=$1 AND management_company_id=$2`, id, mcID)
	if err != nil {
		return false, fmt.Errorf("schedules: delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListPoints возвращает gate/barrier точки УК + их окна (экран расписаний).
func (r *Repo) ListPoints(ctx context.Context, mcID string) ([]PointSchedules, error) {
	const q = `
		SELECT ap.public_id::text, ap.label, ap.type,
		       COALESCE(s.id::text,''), COALESCE(s.dow,0),
		       COALESCE(to_char(s.opens,'HH24:MI'),''), COALESCE(to_char(s.closes,'HH24:MI'),''),
		       COALESCE(s.timezone,''), COALESCE(s.is_active,false)
		FROM access_points ap
		LEFT JOIN access_point_schedules s ON s.access_point_id = ap.id
		WHERE ap.management_company_id = $1 AND ap.type IN ('gate','barrier') AND ap.is_active = true
		ORDER BY ap.label, s.dow, s.opens`
	rows, err := r.pool.Query(ctx, q, mcID)
	if err != nil {
		return nil, fmt.Errorf("schedules: list points: %w", err)
	}
	defer rows.Close()

	var out []PointSchedules
	idx := map[string]int{}
	for rows.Next() {
		var pub, label, typ, sid, opens, closes, tz string
		var dow int
		var active bool
		if err := rows.Scan(&pub, &label, &typ, &sid, &dow, &opens, &closes, &tz, &active); err != nil {
			return nil, fmt.Errorf("schedules: scan point: %w", err)
		}
		i, ok := idx[pub]
		if !ok {
			i = len(out)
			idx[pub] = i
			out = append(out, PointSchedules{PublicID: pub, Label: label, Type: typ})
		}
		if sid != "" {
			out[i].Schedules = append(out[i].Schedules, Row{ID: sid, Dow: dow, Opens: opens, Closes: closes, Timezone: tz, IsActive: active})
		}
	}
	return out, rows.Err()
}

// DueAll возвращает ВСЕ активные окна с устройством точки. Фильтрация «активно
// сейчас» — в reconciler (ActiveAt, tz-логика в Go).
func (r *Repo) DueAll(ctx context.Context) ([]DueRow, error) {
	const q = `
		SELECT s.access_point_id::text, d.id::text, ap.management_company_id::text,
		       s.dow, to_char(s.opens,'HH24:MI'), to_char(s.closes,'HH24:MI'), s.timezone
		FROM access_point_schedules s
		JOIN access_points ap ON ap.id = s.access_point_id AND ap.is_active = true
		JOIN devices d ON d.access_point_id = ap.id
		WHERE s.is_active = true`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("schedules: due all: %w", err)
	}
	defer rows.Close()
	var out []DueRow
	for rows.Next() {
		var d DueRow
		if err := rows.Scan(&d.AccessPointID, &d.DeviceID, &d.MCID, &d.Sched.Dow, &d.Sched.Opens, &d.Sched.Closes, &d.Sched.Timezone); err != nil {
			return nil, fmt.Errorf("schedules: scan due: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
