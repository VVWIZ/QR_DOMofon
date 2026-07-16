package guests

import (
	"testing"
	"time"

	"domofon/backend/internal/platform/httpx"
)

var now = time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

func activeGuest() Guest {
	return Guest{
		TokenHash: "deadbeef",
		ValidFrom: now.Add(-1 * time.Hour),
		ValidTo:   now.Add(1 * time.Hour),
		RevokedAt: nil,
	}
}

func TestGuestValidate_Active(t *testing.T) {
	if herr := activeGuest().Validate(now); herr != nil {
		t.Fatalf("Validate(активный) = %+v, want nil", herr)
	}
}

func TestGuestValidate_Revoked(t *testing.T) {
	rev := now.Add(-10 * time.Minute)
	g := activeGuest()
	g.RevokedAt = &rev
	herr := g.Validate(now)
	if herr == nil || herr.Code != httpx.CodeGuestExpired {
		t.Fatalf("Validate(отозван) = %+v, want GUEST_EXPIRED", herr)
	}
}

func TestGuestValidate_NotYetActive(t *testing.T) {
	g := activeGuest()
	g.ValidFrom = now.Add(1 * time.Hour) // начнётся в будущем
	herr := g.Validate(now)
	if herr == nil || herr.Code != httpx.CodeGuestExpired {
		t.Fatalf("Validate(ещё не начался) = %+v, want GUEST_EXPIRED", herr)
	}
}

func TestGuestValidate_Expired(t *testing.T) {
	g := activeGuest()
	g.ValidTo = now.Add(-1 * time.Second) // истёк секунду назад
	herr := g.Validate(now)
	if herr == nil || herr.Code != httpx.CodeGuestExpired {
		t.Fatalf("Validate(истёк) = %+v, want GUEST_EXPIRED", herr)
	}
}

func TestGuestValidate_ExpiryBoundaryInclusive(t *testing.T) {
	// now == ValidTo → уже недоступен (окно полуоткрытое [from, to)).
	g := activeGuest()
	g.ValidTo = now
	if herr := g.Validate(now); herr == nil {
		t.Fatalf("Validate(now==ValidTo) = nil, want GUEST_EXPIRED")
	}
}

func TestValidateWindow_OK(t *testing.T) {
	if herr := ValidateWindow(now, now.Add(24*time.Hour)); herr != nil {
		t.Fatalf("ValidateWindow(1 день) = %+v, want nil", herr)
	}
}

func TestValidateWindow_MaxBoundary(t *testing.T) {
	// Ровно 2 дня — допустимо.
	if herr := ValidateWindow(now, now.Add(MaxGuestDuration)); herr != nil {
		t.Fatalf("ValidateWindow(ровно 2 дня) = %+v, want nil", herr)
	}
}

func TestValidateWindow_TooLong(t *testing.T) {
	herr := ValidateWindow(now, now.Add(MaxGuestDuration+time.Minute))
	if herr == nil || herr.Code != httpx.CodeValidationError {
		t.Fatalf("ValidateWindow(>2 дней) = %+v, want VALIDATION_ERROR", herr)
	}
}

func TestValidateWindow_Inverted(t *testing.T) {
	herr := ValidateWindow(now, now.Add(-time.Hour))
	if herr == nil || herr.Code != httpx.CodeValidationError {
		t.Fatalf("ValidateWindow(to<from) = %+v, want VALIDATION_ERROR", herr)
	}
}
