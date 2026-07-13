// Package onboarding — самостоятельный онбординг жильцов по одноразовым
// инвайт-ссылкам и выдача постоянных грантов доступа (онбординг + гранты).
//
// Секрет ссылки (raw-токен) отдаётся владельцу и НИКОГДА не хранится в БД в
// открытом виде: хранится только SHA-256-хеш (HashToken), сверка идёт по хешу.
// Здесь — детерминируемая крипто-механика; find-or-create/claim инвайта в БД —
// на стороне репозитория (pgx), вне юнит-контракта этого пакета.
package onboarding

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateToken генерирует новый секрет инвайт-ссылки: 32 случайных байта
// (crypto/rand) в base64url без паддинга (~43 симв., URL-safe). raw отдаётся
// пользователю; в БД кладётся только HashToken(raw).
func GenerateToken() (raw string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken возвращает SHA-256 от raw в hex (64 симв.). Детерминирован: один и
// тот же raw всегда даёт один хеш — по нему идёт lookup инвайта. Необратим:
// HashToken(raw) != raw.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
