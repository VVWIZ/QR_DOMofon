package auth

import (
	"context"
	"time"
)

// refreshKV — минимальный Redis-подобный KV для whitelist refresh-токенов
// (боевая реализация — go-redis; в тестах — map-фейк). Логика ротации/детекта
// reuse — в RefreshWhitelist (чистая логика, тестируемая без Redis).
type refreshKV interface {
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	Get(ctx context.Context, key string) (val string, ok bool, err error)
	Del(ctx context.Context, key string) error
}

// RefreshWhitelist — whitelist активных refresh-jti (auth.md §3): ключ
// auth:refresh:{jti} → user_id, TTL = RefreshTTL. Whitelist (не blacklist):
// единая точка отзыва и детект повторного использования украденного токена.
type RefreshWhitelist struct {
	kv refreshKV
}

// NewRefreshWhitelist собирает whitelist поверх KV.
func NewRefreshWhitelist(kv refreshKV) *RefreshWhitelist {
	return &RefreshWhitelist{kv: kv}
}

// Issue добавляет jti в whitelist (SET auth:refresh:{jti} = userID, TTL).
func (w *RefreshWhitelist) Issue(ctx context.Context, jti, userID string, ttl time.Duration) error {
	panic("not implemented: auth.RefreshWhitelist.Issue")
}

// Validate возвращает userID для jti, если тот в whitelist; ok=false — если нет
// (истёк, отозван или уже ротирован → детект reuse на вызывающей стороне).
func (w *RefreshWhitelist) Validate(ctx context.Context, jti string) (userID string, ok bool, err error) {
	panic("not implemented: auth.RefreshWhitelist.Validate")
}

// Rotate атомарно заменяет oldJTI на newJTI для userID: проверяет наличие
// oldJTI в whitelist (иначе ошибка — reuse/отзыв), удаляет old и добавляет new
// с TTL. После ротации oldJTI невалиден.
func (w *RefreshWhitelist) Rotate(ctx context.Context, oldJTI, newJTI, userID string, ttl time.Duration) error {
	panic("not implemented: auth.RefreshWhitelist.Rotate")
}

// Revoke удаляет jti из whitelist (DEL). Идемпотентно (logout).
func (w *RefreshWhitelist) Revoke(ctx context.Context, jti string) error {
	panic("not implemented: auth.RefreshWhitelist.Revoke")
}
