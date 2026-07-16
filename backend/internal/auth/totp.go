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

// GenerateTOTPSecret создаёт новый TOTP-секрет для аккаунта account (email).
// Возвращает base32-секрет (в БД, users.totp_secret) и otpauth://-URI для QR/ввода
// в приложение-аутентификатор (отдаётся создателю ОДИН раз, в логи/аудит не пишется).
func GenerateTOTPSecret(account string) (secret, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "QR-Domofon",
		AccountName: account,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}
