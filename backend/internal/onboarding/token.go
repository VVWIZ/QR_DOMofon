// Package onboarding — самостоятельный онбординг жильцов по одноразовым
// инвайт-ссылкам и выдача постоянных грантов доступа (онбординг + гранты).
//
// Секрет ссылки (raw-токен) отдаётся владельцу и НИКОГДА не хранится в БД в
// открытом виде: хранится только SHA-256-хеш (HashToken), сверка идёт по хешу.
// Здесь — детерминируемая крипто-механика; find-or-create/claim инвайта в БД —
// на стороне репозитория (pgx), вне юнит-контракта этого пакета.
package onboarding

// GenerateToken генерирует новый секрет инвайт-ссылки: 32 случайных байта
// (crypto/rand) в base64url без паддинга (~43 симв., URL-safe). raw отдаётся
// пользователю; в БД кладётся только HashToken(raw).
func GenerateToken() (raw string, err error) {
	panic("not implemented: onboarding.GenerateToken")
}

// HashToken возвращает SHA-256 от raw в hex (64 симв.). Детерминирован: один и
// тот же raw всегда даёт один хеш — по нему идёт lookup инвайта. Необратим:
// HashToken(raw) != raw.
func HashToken(raw string) string {
	panic("not implemented: onboarding.HashToken")
}
