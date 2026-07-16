package guests

import (
	"time"

	"domofon/backend/internal/platform/httpx"
)

// MaxGuestDuration — максимальное окно гостевого доступа (ТЗ §3.5: гость ≤ 2 дней).
const MaxGuestDuration = 48 * time.Hour

// Guest — состояние гостевого доступа, достаточное для проверки валидности окна
// без БД. Секрет в открытом виде не хранится (TokenHash — SHA-256).
type Guest struct {
	TokenHash string
	ValidFrom time.Time
	ValidTo   time.Time
	RevokedAt *time.Time // nil, пока не отозван
}

// Validate проверяет пригодность гостевого доступа на момент now:
//
//   - отозван (RevokedAt != nil)      → GUEST_EXPIRED (410);
//   - ещё не начался (now < ValidFrom) → GUEST_EXPIRED (410);
//   - истёк (now >= ValidTo)          → GUEST_EXPIRED (410);
//   - иначе                           → nil (в окне).
//
// «Не найден/чужой токен» — забота репозитория (GUEST_INVALID 404); здесь только
// окно/отзыв уже найденного доступа. now инъектируется для детерминизма.
func (g Guest) Validate(now time.Time) *httpx.Error {
	if g.RevokedAt != nil {
		return httpx.NewError(httpx.CodeGuestExpired, "Guest access has been revoked")
	}
	if now.Before(g.ValidFrom) {
		return httpx.NewError(httpx.CodeGuestExpired, "Guest access is not active yet")
	}
	if !now.Before(g.ValidTo) {
		return httpx.NewError(httpx.CodeGuestExpired, "Guest access has expired")
	}
	return nil
}

// ValidateWindow проверяет запрошенное окно [from, to] при СОЗДАНИИ гостя:
// to > from и длительность ≤ MaxGuestDuration. Иначе → VALIDATION_ERROR.
// Дублирует CHECK-и БД человекочитаемым ответом (не 500 от constraint).
func ValidateWindow(from, to time.Time) *httpx.Error {
	if !to.After(from) {
		return httpx.NewError(httpx.CodeValidationError, "valid_to must be after valid_from")
	}
	if to.Sub(from) > MaxGuestDuration {
		return httpx.NewError(httpx.CodeValidationError, "Guest access window must not exceed 2 days")
	}
	return nil
}
