// Package audit — append-only журнал событий (architecture.md §3, AC14). Код
// выполняет только INSERT/SELECT; UPDATE/DELETE отсутствуют. Event —
// кросс-модульный контракт события (аналог схемы события шины): модули calls,
// access, devices эмитят его через интерфейс Recorder, а транспорт (запись в
// Postgres) остаётся заменяемым.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event — запись аудита. UUID-поля строковые: пустая строка → NULL в БД.
type Event struct {
	EventType           string
	Actor               string
	ApartmentID         string
	AccessPointID       string
	DeviceID            string
	CallID              string
	RequestID           string
	ManagementCompanyID string
	Metadata            map[string]any
}

// Recorder — интерфейс записи события. Потребители зависят от него, а не от
// конкретной реализации PgRecorder.
type Recorder interface {
	Record(ctx context.Context, ev Event) error
}

// Row — прочитанное событие (для GET /api/v1/audit/events).
type Row struct {
	ID                  int64
	EventType           string
	OccurredAt          time.Time
	Actor               *string
	ApartmentID         *string
	AccessPointID       *string
	DeviceID            *string
	CallID              *string
	RequestID           *string
	ManagementCompanyID *string
	Metadata            json.RawMessage
}

// defaultLimit / maxLimit — пагинация чтения (api.md GET /audit/events).
const (
	defaultLimit = 50
	maxLimit     = 500
)

// PgRecorder — Postgres-реализация Recorder поверх pgxpool.
type PgRecorder struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewRecorder создаёт PgRecorder.
func NewRecorder(pool *pgxpool.Pool, log *slog.Logger) *PgRecorder {
	return &PgRecorder{pool: pool, log: log}
}

// Record добавляет событие (INSERT, occurred_at = now()). Ошибка логируется и
// возвращается вызывающему, но по контракту skeleton не должна валить основной
// поток (аудит best-effort на стороне вызова).
func (r *PgRecorder) Record(ctx context.Context, ev Event) error {
	var meta any
	if ev.Metadata != nil {
		b, err := json.Marshal(ev.Metadata)
		if err != nil {
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
		meta = string(b)
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_events
			(event_type, occurred_at, actor, apartment_id, access_point_id,
			 device_id, call_id, request_id, management_company_id, metadata)
		VALUES ($1, now(), $2, $3, $4, $5, $6, $7, $8, $9)`,
		ev.EventType,
		nullStr(ev.Actor),
		nullUUID(ev.ApartmentID),
		nullUUID(ev.AccessPointID),
		nullUUID(ev.DeviceID),
		nullUUID(ev.CallID),
		nullUUID(ev.RequestID),
		nullUUID(ev.ManagementCompanyID),
		meta,
	)
	if err != nil {
		r.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

// List возвращает последние события (новые первыми). limit нормализуется в
// [1, maxLimit], 0/отрицательное → defaultLimit.
func (r *PgRecorder) List(ctx context.Context, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, event_type, occurred_at, actor, apartment_id, access_point_id,
		       device_id, call_id, request_id, management_company_id, metadata
		FROM audit_events
		ORDER BY id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(
			&row.ID, &row.EventType, &row.OccurredAt, &row.Actor,
			&row.ApartmentID, &row.AccessPointID, &row.DeviceID, &row.CallID,
			&row.RequestID, &row.ManagementCompanyID, &row.Metadata,
		); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: rows: %w", err)
	}
	return out, nil
}

// nullUUID: пустая строка → NULL, иначе строковый UUID (pgx кастует в uuid).
func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullStr: пустая строка → NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
