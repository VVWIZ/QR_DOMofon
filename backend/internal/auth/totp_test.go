package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// devSecret — TOTP-секрет фикстуры УК-админа (auth.md §7, base32).
const devSecret = "JBSWY3DPEHPK3PXP"

// fixedTOTPTime — фиксированный момент, чтобы код был воспроизводим без sleep.
var fixedTOTPTime = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func TestVerifyTOTP_ValidCode(t *testing.T) {
	code, err := totp.GenerateCode(devSecret, fixedTOTPTime)
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	if !VerifyTOTP(devSecret, code, fixedTOTPTime) {
		t.Fatalf("VerifyTOTP(валидный код %q) = false, want true", code)
	}
}

func TestVerifyTOTP_WrongCode(t *testing.T) {
	if VerifyTOTP(devSecret, "000000", fixedTOTPTime) {
		t.Fatalf("VerifyTOTP(неверный код) = true, want false")
	}
}
