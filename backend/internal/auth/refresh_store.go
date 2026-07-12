package auth

import (
	"context"
	"errors"
	"time"
)

// refreshKeyPrefix — префикс ключа whitelist в KV (auth.md §3).
const refreshKeyPrefix = "auth:refresh:"

// refreshKey строит ключ whitelist для jti.
func refreshKey(jti string) string { return refreshKeyPrefix + jti }

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
	return w.kv.Set(ctx, refreshKey(jti), userID, ttl)
}

// Validate возвращает userID для jti, если тот в whitelist; ok=false — если нет
// (истёк, отозван или уже ротирован → детект reuse на вызывающей стороне).
func (w *RefreshWhitelist) Validate(ctx context.Context, jti string) (userID string, ok bool, err error) {
	return w.kv.Get(ctx, refreshKey(jti))
}

// Rotate атомарно заменяет oldJTI на newJTI для userID: проверяет наличие
// oldJTI в whitelist (иначе ошибка — reuse/отзыв), удаляет old и добавляет new
// с TTL. После ротации oldJTI невалиден.
func (w *RefreshWhitelist) Rotate(ctx context.Context, oldJTI, newJTI, userID string, ttl time.Duration) error {
	_, ok, err := w.kv.Get(ctx, refreshKey(oldJTI))
	if err != nil {
		return err
	}
	if !ok {
		// Старого jti нет в whitelist → отозван или это повторное использование
		// украденного токена (детект reuse, auth.md §3).
		return errors.New("refresh jti not in whitelist (reuse or revoked)")
	}
	if err := w.kv.Del(ctx, refreshKey(oldJTI)); err != nil {
		return err
	}
	return w.kv.Set(ctx, refreshKey(newJTI), userID, ttl)
}

// Revoke удаляет jti из whitelist (DEL). Идемпотентно (logout).
func (w *RefreshWhitelist) Revoke(ctx context.Context, jti string) error {
	return w.kv.Del(ctx, refreshKey(jti))
}
