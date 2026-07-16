// Package guests — гостевой доступ (ТЗ §2.2.8, §3.1.5, §4.6): временный доступ на
// точки по ссылке /g/{token} без аккаунта, ограниченный окном ≤ 2 дней. Доступ
// ПРОИЗВОДНЫЙ от прав создателя (владельца/жильца с can_create_guests) и
// переспрашивается на каждом открытии.
//
// Границы: модуль владеет таблицами guest_access/guest_access_points и публичным
// входом /g/*; открытие точки делегируется access-машинерии через consumer-side
// интерфейс DoorOpener (не импортируя access напрямую в доменную логику).
package guests

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateToken генерирует секрет гостевой ссылки: 32 случайных байта → base64url
// без паддинга (~43 симв.). raw отдаётся создателю; в БД — только HashToken(raw).
//
// NB: дублирует onboarding.GenerateToken/HashToken (та же механика). Вынос в
// internal/platform/token — долг следующего инкремента; здесь копия, чтобы не
// трогать onboarding и его тесты.
func GenerateToken() (raw string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken возвращает SHA-256 от raw в hex (64 симв.) — по нему идёт lookup
// гостя. Необратим.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
