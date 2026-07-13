package onboarding

import (
	"testing"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// now — фиксированный момент проверки (инъекция).
var now = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func validInvite() Invite {
	return Invite{
		TokenHash:   "deadbeef",
		ApartmentID: "33333333-3333-3333-3333-333333333333",
		Role:        "resident",
		UsedAt:      nil,
		ExpiresAt:   now.Add(1 * time.Hour), // ещё не истёк
	}
}

func TestInviteValidate_Valid(t *testing.T) {
	if herr := validInvite().Validate(now); herr != nil {
		t.Fatalf("Validate(валидный инвайт) = %+v, want nil", herr)
	}
}

func TestInviteValidate_UsedIsInvalid(t *testing.T) {
	used := now.Add(-10 * time.Minute)
	inv := validInvite()
	inv.UsedAt = &used

	herr := inv.Validate(now)
	if herr == nil {
		t.Fatalf("Validate(использованный инвайт) = nil, want INVITE_INVALID")
	}
	if herr.Code != httpx.CodeInviteInvalid {
		t.Errorf("code = %q, want %q", herr.Code, httpx.CodeInviteInvalid)
	}
}

func TestInviteValidate_ExpiredIsGone(t *testing.T) {
	inv := validInvite()
	inv.ExpiresAt = now.Add(-1 * time.Second) // истёк секунду назад

	herr := inv.Validate(now)
	if herr == nil {
		t.Fatalf("Validate(просроченный инвайт) = nil, want INVITE_EXPIRED")
	}
	if herr.Code != httpx.CodeInviteExpired {
		t.Errorf("code = %q, want %q", herr.Code, httpx.CodeInviteExpired)
	}
}
