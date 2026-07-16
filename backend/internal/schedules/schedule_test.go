package schedules

import (
	"testing"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// tzAlmaty — UTC+5 без DST (удобно для детерминизма).
const tzAlmaty = "Asia/Almaty"

func TestActiveAt_InsideWindow(t *testing.T) {
	// Ср 2026-07-15 10:00 по Алматы = 05:00 UTC.
	now := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	s := Schedule{Dow: int(time.Wednesday), Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty}
	if !s.ActiveAt(now) {
		t.Fatalf("ActiveAt(внутри окна) = false, want true")
	}
}

func TestActiveAt_BeforeOpen(t *testing.T) {
	// 07:00 по Алматы = 02:00 UTC, окно с 08:00.
	now := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	s := Schedule{Dow: int(time.Wednesday), Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty}
	if s.ActiveAt(now) {
		t.Fatalf("ActiveAt(до открытия) = true, want false")
	}
}

func TestActiveAt_AtCloseExclusive(t *testing.T) {
	// Ровно 22:00 по Алматы = 17:00 UTC — окно [08:00,22:00) уже закрыто.
	now := time.Date(2026, 7, 15, 17, 0, 0, 0, time.UTC)
	s := Schedule{Dow: int(time.Wednesday), Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty}
	if s.ActiveAt(now) {
		t.Fatalf("ActiveAt(ровно closes) = true, want false (полуоткрытое окно)")
	}
}

func TestActiveAt_WrongWeekday(t *testing.T) {
	// Тот же час, но окно на другой день недели.
	now := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC) // среда
	s := Schedule{Dow: int(time.Monday), Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty}
	if s.ActiveAt(now) {
		t.Fatalf("ActiveAt(чужой день недели) = true, want false")
	}
}

func TestActiveAt_TimezoneShiftsWeekday(t *testing.T) {
	// 23:30 UTC среды = 04:30 четверга по Алматы (+5). Окно на четверг утром.
	now := time.Date(2026, 7, 15, 23, 30, 0, 0, time.UTC)
	s := Schedule{Dow: int(time.Thursday), Opens: "04:00", Closes: "06:00", Timezone: tzAlmaty}
	if !s.ActiveAt(now) {
		t.Fatalf("ActiveAt(окно в локальный чт по сдвигу tz) = false, want true")
	}
}

func TestActiveAt_BadTimezone(t *testing.T) {
	s := Schedule{Dow: 3, Opens: "08:00", Closes: "22:00", Timezone: "Nowhere/Nope"}
	if s.ActiveAt(time.Now()) {
		t.Fatalf("ActiveAt(битая tz) = true, want false")
	}
}

func TestValidateSchedule_OK(t *testing.T) {
	if herr := ValidateSchedule(Schedule{Dow: 1, Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty}); herr != nil {
		t.Fatalf("ValidateSchedule(валидное) = %+v, want nil", herr)
	}
}

func TestValidateSchedule_Errors(t *testing.T) {
	cases := []Schedule{
		{Dow: 7, Opens: "08:00", Closes: "22:00", Timezone: tzAlmaty},   // dow вне 0..6
		{Dow: 1, Opens: "9am", Closes: "22:00", Timezone: tzAlmaty},     // формат
		{Dow: 1, Opens: "22:00", Closes: "08:00", Timezone: tzAlmaty},   // closes<=opens
		{Dow: 1, Opens: "08:00", Closes: "22:00", Timezone: "Bad/Zone"}, // tz
	}
	for i, c := range cases {
		if herr := ValidateSchedule(c); herr == nil || herr.Code != httpx.CodeValidationError {
			t.Errorf("случай %d: ValidateSchedule = %+v, want VALIDATION_ERROR", i, herr)
		}
	}
}
