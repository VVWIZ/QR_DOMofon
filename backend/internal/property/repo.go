// Package property — чтение иерархии УК → дом → квартира → AccessPoint →
// устройство (architecture.md §1, §3). Владелец таблиц property. По публичному
// public_id точки доступа (значение aid в QR) возвращает контекст для валидации
// QR, инициации звонка и открытия двери. Внутренний access_points.id наружу не
// отдаётся (ТЗ §17.4).
package property

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound — точка доступа по public_id не найдена, неактивна, или у неё нет
// активной квартиры/устройства (наружу маппится в INVALID_QR).
var ErrNotFound = errors.New("property: not found")

// AccessPoint — точка доступа (наружу отдаётся public_id + label).
type AccessPoint struct {
	ID       string
	PublicID string
	Label    string
	Type     string
}

// Apartment — квартира-адресат звонка.
type Apartment struct {
	ID     string
	Number string
}

// Device — устройство точки доступа.
type Device struct {
	ID              string
	Serial          string
	Type            string
	FirmwareVersion string
	LastSeenAt      *time.Time
}

// Context — разрешённый по public_id контекст точки доступа.
type Context struct {
	ManagementCompanyID string
	BuildingID          string
	AccessPoint         AccessPoint
	Apartment           Apartment
	Device              Device
}

// Repo — доступ к таблицам property поверх pgxpool.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий property.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// ResolveByPublicID возвращает контекст точки доступа по её public_id.
//
// В skeleton у дома одна активная квартира-фикстура и одно устройство на точку,
// поэтому берём активную квартиру дома и устройство точки. Нет строки →
// ErrNotFound.
func (r *Repo) ResolveByPublicID(ctx context.Context, publicID string) (Context, error) {
	// public_id — колонка типа uuid; не-UUID вход дал бы ошибку каста (→ 500).
	// Трактуем как «не найдено» (наружу INVALID_QR), а не как внутреннюю ошибку.
	if _, err := uuid.Parse(publicID); err != nil {
		return Context{}, ErrNotFound
	}

	const q = `
		SELECT ap.id, ap.public_id, ap.label, ap.type,
		       ap.management_company_id, ap.building_id,
		       a.id, a.number,
		       d.id, d.serial, d.type, d.firmware_version, d.last_seen_at
		FROM access_points ap
		JOIN apartments a
		    ON a.building_id = ap.building_id AND a.is_active = true
		JOIN devices d
		    ON d.access_point_id = ap.id
		WHERE ap.public_id = $1 AND ap.is_active = true
		ORDER BY a.number
		LIMIT 1`

	var c Context
	err := r.pool.QueryRow(ctx, q, publicID).Scan(
		&c.AccessPoint.ID, &c.AccessPoint.PublicID, &c.AccessPoint.Label, &c.AccessPoint.Type,
		&c.ManagementCompanyID, &c.BuildingID,
		&c.Apartment.ID, &c.Apartment.Number,
		&c.Device.ID, &c.Device.Serial, &c.Device.Type, &c.Device.FirmwareVersion, &c.Device.LastSeenAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Context{}, ErrNotFound
	}
	if err != nil {
		return Context{}, fmt.Errorf("property: resolve public_id: %w", err)
	}
	return c, nil
}
