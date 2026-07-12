package devices

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Device — строка реестра устройств (без derived-статуса; статус добавляет
// хендлер из presence).
type Device struct {
	ID              string
	Serial          string
	AccessPointID   string
	Type            string
	FirmwareVersion string
	LastSeenAt      *time.Time
}

// DeviceContext — контекст устройства для обогащения аудита событий
// (fail_open_*): к какой точке/квартире/УК относится устройство.
type DeviceContext struct {
	AccessPointID       string
	ApartmentID         string
	ManagementCompanyID string
}

// Repo — доступ к таблице devices поверх pgxpool.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий устройств.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// List возвращает устройства управляющей компании mcID (скоуп admin по
// management_company_id из claims, auth.md §5). Сравнение через ::text — пустой
// mcID даёт пустой результат без ошибки каста uuid.
func (r *Repo) List(ctx context.Context, mcID string) ([]Device, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, serial, access_point_id, type, firmware_version, last_seen_at
		FROM devices
		WHERE management_company_id::text = $1
		ORDER BY serial`, mcID)
	if err != nil {
		return nil, fmt.Errorf("devices: list: %w", err)
	}
	defer rows.Close()

	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Serial, &d.AccessPointID, &d.Type, &d.FirmwareVersion, &d.LastSeenAt); err != nil {
			return nil, fmt.Errorf("devices: scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("devices: rows: %w", err)
	}
	return out, nil
}

// UpdateLastSeen обновляет last_seen_at устройства (на каждый heartbeat).
func (r *Repo) UpdateLastSeen(ctx context.Context, deviceID string, seenAt time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, deviceID, seenAt)
	if err != nil {
		return fmt.Errorf("devices: update last_seen: %w", err)
	}
	return nil
}

// Context возвращает контекст устройства (точка/квартира/УК) для аудита. Берёт
// активную квартиру дома точки доступа (в skeleton — одна фикстура).
func (r *Repo) Context(ctx context.Context, deviceID string) (DeviceContext, bool, error) {
	const q = `
		SELECT ap.id, ap.management_company_id, a.id
		FROM devices d
		JOIN access_points ap ON ap.id = d.access_point_id
		JOIN apartments a ON a.building_id = ap.building_id AND a.is_active = true
		WHERE d.id = $1
		ORDER BY a.number
		LIMIT 1`

	var dc DeviceContext
	err := r.pool.QueryRow(ctx, q, deviceID).Scan(&dc.AccessPointID, &dc.ManagementCompanyID, &dc.ApartmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceContext{}, false, nil
	}
	if err != nil {
		return DeviceContext{}, false, fmt.Errorf("devices: context: %w", err)
	}
	return dc, true, nil
}
