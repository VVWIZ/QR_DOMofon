package auth

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// VerifyTOTP проверяет TOTP-код code для секрета secret (base32) на момент now.
// true — код валиден (с учётом стандартного окна ±1 шаг), иначе false.
// now инъектируется → тест детерминирован без ожидания реального времени.
func VerifyTOTP(secret, code string, now time.Time) bool {
	ok, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false
	}
	return ok
}
