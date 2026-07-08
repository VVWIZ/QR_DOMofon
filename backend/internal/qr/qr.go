// Package qr валидирует HMAC-подпись QR-URL посетителя.
//
// Контракт (docs/skeleton/architecture.md §5, api.md "POST /api/v1/qr/validate",
// ТЗ §5.3):
//
//	message = aid + ":" + v + ":" + kid
//	sig     = base64url(HMAC-SHA256(message, secret[kid]))[0:32]   // без padding
//
// Сравнение подписи обязано быть константным по времени (hmac.Equal), чтобы не
// утекала информация о правильных префиксах через тайминг.
//
// СКЕЛЕТ ЭТАПА QA: тела функций паникуют ("not implemented"). Реализацию пишет
// этап backend — здесь зафиксирован только контракт (сигнатуры + ошибки), под
// который написаны RED-тесты.
package qr

import "errors"

// Ошибки валидации. Наружу (клиенту) обе маппятся в единый INVALID_QR — причина
// клиенту не раскрывается, пишется только в лог backend (api.md, лог
// qr_validation_failed). Раздельные sentinel'ы нужны для точного логирования и
// для юнит-контракта.
var (
	// ErrInvalidSignature — подпись не совпала с ожидаемой (битая/чужая/чужой aid|v).
	ErrInvalidSignature = errors.New("qr: invalid signature")
	// ErrUnknownKID — kid отсутствует в реестре ключей (или ключ неактивен).
	ErrUnknownKID = errors.New("qr: unknown kid")
)

// Keyring — реестр секретов подписи QR по kid (таблица qr_keys, ротация по kid).
// Интерфейс объявлен на стороне потребителя (пакет qr) по правилу границ
// модулей из architecture.md §1: конкретную реализацию (адаптер к Postgres)
// внедряет cmd/server, а в тестах используется фейк на map.
type Keyring interface {
	// Secret возвращает секрет для kid; ok=false, если kid неизвестен или
	// соответствующий ключ неактивен.
	Secret(kid string) (secret string, ok bool)
}

// Sign вычисляет каноническую подпись QR для (aid, v, kid) секретом secret:
// base64url(HMAC-SHA256("aid:v:kid", secret)) без padding, первые 32 символа.
func Sign(aid, v, kid, secret string) string {
	panic("not implemented: qr.Sign")
}

// Verify проверяет подпись sig для (aid, v, kid), беря секрет из keyring по kid:
//
//   - kid не найден в keyring       → ErrUnknownKID;
//   - sig не совпал (сравнение константное по времени) → ErrInvalidSignature;
//   - подпись верна                 → nil.
func Verify(aid, v, kid, sig string, keyring Keyring) error {
	panic("not implemented: qr.Verify")
}
