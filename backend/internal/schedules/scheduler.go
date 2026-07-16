package schedules

// Reconciler — level-based планировщик авто-открытия. Каждый тик вычисляет
// желаемое состояние точек («окно активно сейчас?») и для тех, что должны быть
// открыты, ПЕРЕИЗДАЁТ короткую аренду open_relay (fresh request_id, duration >
// tick). «Держим открытым» = продлеваем аренду каждый тик, команду «закрыть» НЕ
// шлём. Отказ планировщика/сервера → аренда истекает, реле закрывается само
// (fail-secure). pg advisory-lock не даёт двум инстансам дублировать команды.

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"domofon/backend/internal/audit"
)

// advisoryLockKey — ключ pg_try_advisory_lock (защита от двойного инстанса).
const advisoryLockKey int64 = 0x5CED0001

const relayID = 1

// OpenCommand — команда открытия реле (публикуется адаптером Publisher).
type OpenCommand struct {
	RelayID    int
	DurationMs int
	RequestID  string
	IssuedBy   string
	IssuedAt   string
}

// Publisher публикует команду устройству (адаптер поверх devices.Commander).
type Publisher interface {
	PublishOpenRelay(ctx context.Context, deviceID string, cmd OpenCommand) error
}

// Presence сообщает online-статус устройства (пропускаем offline).
type Presence interface {
	IsOnline(ctx context.Context, deviceID string) (bool, error)
}

// Reconciler — планировщик.
type Reconciler struct {
	pool      *pgxpool.Pool
	repo      *Repo
	publisher Publisher
	presence  Presence
	audit     audit.Recorder
	log       *slog.Logger
	tick      time.Duration
	lease     time.Duration
	now       func() time.Time

	// open — состояние окон предыдущего тика (access_point_id → открыто), для
	// аудита переходов (started/ended). В памяти: при рестарте возможен повторный
	// started — приемлемо.
	open map[string]bool
}

// NewReconciler собирает планировщик. lease должен быть > tick (перекрытие аренд).
func NewReconciler(pool *pgxpool.Pool, repo *Repo, pub Publisher, presence Presence, recorder audit.Recorder, tick, lease time.Duration, log *slog.Logger) *Reconciler {
	return &Reconciler{
		pool: pool, repo: repo, publisher: pub, presence: presence, audit: recorder,
		log: log, tick: tick, lease: lease, now: time.Now, open: map[string]bool{},
	}
}

// Run запускает цикл до отмены ctx (вызывать в отдельной горутине).
func (rc *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(rc.tick)
	defer t.Stop()
	rc.log.Info("scheduler_started", "tick", rc.tick.String(), "lease", rc.lease.String())
	for {
		select {
		case <-ctx.Done():
			rc.log.Info("scheduler_stopped")
			return
		case <-t.C:
			rc.reconcile(ctx)
		}
	}
}

// reconcile — один проход: под advisory-lock вычислить активные окна и переиздать
// аренды.
func (rc *Reconciler) reconcile(ctx context.Context) {
	conn, err := rc.pool.Acquire(ctx)
	if err != nil {
		rc.log.Error("scheduler_acquire_failed", "error", err)
		return
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&locked); err != nil {
		rc.log.Error("scheduler_lock_failed", "error", err)
		return
	}
	if !locked {
		return // другой инстанс ведёт планирование
	}
	defer func() { _, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey) }()

	due, err := rc.repo.DueAll(ctx)
	if err != nil {
		rc.log.Error("scheduler_due_failed", "error", err)
		return
	}
	now := rc.now()

	// Точки, у которых сейчас активно хотя бы одно окно (dedupe по точке).
	openNow := map[string]string{} // access_point_id → device_id
	mcOf := map[string]string{}
	for _, d := range due {
		if d.Sched.ActiveAt(now) {
			openNow[d.AccessPointID] = d.DeviceID
			mcOf[d.AccessPointID] = d.MCID
		}
	}

	// Переходы для аудита + переиздание аренды активным.
	for apID, deviceID := range openNow {
		if !rc.open[apID] {
			rc.record(ctx, audit.Event{EventType: "scheduled_open_started", Actor: "scheduler", AccessPointID: apID, DeviceID: deviceID, ManagementCompanyID: mcOf[apID]})
		}
		rc.pulse(ctx, apID, deviceID, now)
	}
	// Окна, которые закрылись с прошлого тика.
	for apID := range rc.open {
		if _, still := openNow[apID]; !still {
			rc.record(ctx, audit.Event{EventType: "scheduled_open_ended", Actor: "scheduler", AccessPointID: apID})
		}
	}

	next := make(map[string]bool, len(openNow))
	for apID := range openNow {
		next[apID] = true
	}
	rc.open = next
}

// pulse переиздаёт аренду open_relay точке (если устройство online).
func (rc *Reconciler) pulse(ctx context.Context, apID, deviceID string, now time.Time) {
	online, err := rc.presence.IsOnline(ctx, deviceID)
	if err != nil {
		rc.log.Warn("scheduler_presence_failed", "error", err, "device_id", deviceID)
		return
	}
	if !online {
		rc.log.Warn("scheduled_point_offline", "access_point_id", apID, "device_id", deviceID)
		return
	}
	cmd := OpenCommand{
		RelayID:    relayID,
		DurationMs: int(rc.lease / time.Millisecond),
		RequestID:  uuid.NewString(), // уникальный → эмулятор переактуирует (не дедуп)
		IssuedBy:   "scheduler",
		IssuedAt:   now.UTC().Format(time.RFC3339),
	}
	if err := rc.publisher.PublishOpenRelay(ctx, deviceID, cmd); err != nil {
		rc.log.Error("scheduler_publish_failed", "error", err, "device_id", deviceID)
	}
}

func (rc *Reconciler) record(ctx context.Context, ev audit.Event) {
	if rc.audit == nil {
		return
	}
	if err := rc.audit.Record(ctx, ev); err != nil {
		rc.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
	}
}
