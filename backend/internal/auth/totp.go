package auth

import (
	"time"

	"github.com/pquerna/otp/totp"
)

// keep github.com/pquerna/otp direct-зависимостью, пока VerifyTOTP — скелет.
var _ = totp.Validate

// VerifyTOTP проверяет TOTP-код code для секрета secret (base32) на момент now.
// true — код валиден (с учётом стандартного окна ±1 шаг), иначе false.
// now инъектируется → тест детерминирован без ожидания реального времени.
func VerifyTOTP(secret, code string, now time.Time) bool {
	panic("not implemented: auth.VerifyTOTP")
}
