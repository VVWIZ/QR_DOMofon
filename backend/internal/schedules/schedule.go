// Package schedules — авто-открытие точек доступа по расписанию (инкремент E).
// Планировщик держит реле открытым АРЕНДОЙ: короткий open_relay, переиздаваемый
// каждый тик, пока окно активно. Отказ планировщика → аренда истекает, реле
// закрывается само (fail-secure). Здесь — чистая логика окна (тестируема без БД);
// репозиторий, reconciler и HTTP — в остальных файлах пакета.
package schedules

import (
	"time"

	"domofon/backend/internal/platform/httpx"
)

// Schedule — одно окно авто-открытия точки: день недели + [Opens, Closes) в
// таймзоне Timezone.
type Schedule struct {
	Dow      int    // 0=вс … 6=сб (time.Weekday)
	Opens    string // "HH:MM" локального времени
	Closes   string // "HH:MM"
	Timezone string // IANA, напр. "Asia/Almaty"
}

// ActiveAt сообщает, попадает ли момент now (в любой зоне) внутрь окна: в
// таймзоне расписания день недели == Dow и Opens ≤ локальное время < Closes.
// Некорректная таймзона/формат времени → false (окно не активируется).
func (s Schedule) ActiveAt(now time.Time) bool {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return false
	}
	local := now.In(loc)
	if int(local.Weekday()) != s.Dow {
		return false
	}
	opens, ok1 := parseHM(s.Opens)
	closes, ok2 := parseHM(s.Closes)
	if !ok1 || !ok2 {
		return false
	}
	mins := local.Hour()*60 + local.Minute()
	return mins >= opens && mins < closes
}

// ValidateSchedule проверяет параметры создаваемого окна: валидные Dow (0..6),
// время "HH:MM", Closes > Opens, известная таймзона. Иначе → VALIDATION_ERROR.
func ValidateSchedule(s Schedule) *httpx.Error {
	if s.Dow < 0 || s.Dow > 6 {
		return httpx.NewError(httpx.CodeValidationError, "dow must be 0..6")
	}
	opens, ok1 := parseHM(s.Opens)
	closes, ok2 := parseHM(s.Closes)
	if !ok1 || !ok2 {
		return httpx.NewError(httpx.CodeValidationError, "opens/closes must be HH:MM")
	}
	if closes <= opens {
		return httpx.NewError(httpx.CodeValidationError, "closes must be after opens")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return httpx.NewError(httpx.CodeValidationError, "unknown timezone")
	}
	return nil
}

// parseHM разбирает "HH:MM" в минуты от полуночи. ok=false → неверный формат.
func parseHM(v string) (int, bool) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}
