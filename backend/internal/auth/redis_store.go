package auth

// Redis-адаптеры интерфейсов OtpStore и refreshKV (auth.md §3–§4) поверх
// go-redis. Реализуют боевые хранилища; юнит-логика этапа 5a остаётся на
// map-фейках. Клиент go-redis переиспользуется из platform/redis (не дублируем).

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Ключи Redis (auth.md §4). Префиксы отличаются от call:/device: (calls/devices).
func otpKey(phone string) string      { return "otp:" + phone }
func otpReqKey(phone string) string   { return "otp:req:" + phone }
func otpBlockKey(phone string) string { return "blocked:" + phone }

// RedisOtpStore — боевая реализация OtpStore поверх go-redis (ключи otp:{phone},
// otp:req:{phone}, blocked:{phone}). Политика лимитов/блокировок — в OtpService.
type RedisOtpStore struct {
	rdb *redis.Client
}

// NewRedisOtpStore собирает OTP-хранилище поверх клиента go-redis.
func NewRedisOtpStore(rdb *redis.Client) *RedisOtpStore {
	return &RedisOtpStore{rdb: rdb}
}

// IncrRequests атомарно инкрементит счётчик запросов otp:req:{phone}; на первом
// инкременте задаёт TTL = window (auth.md §4: не более OtpMaxRequests / окно).
func (s *RedisOtpStore) IncrRequests(ctx context.Context, phone string, window time.Duration, _ time.Time) (int, error) {
	key := otpReqKey(phone)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err := s.rdb.Expire(ctx, key, window).Err(); err != nil {
			return int(n), err
		}
	}
	return int(n), nil
}

// GetOTP возвращает активную запись OTP (otp:{phone}); отсутствие ключа или
// истечение относительно now → ok=false.
func (s *RedisOtpStore) GetOTP(ctx context.Context, phone string, now time.Time) (OtpRecord, bool, error) {
	body, err := s.rdb.Get(ctx, otpKey(phone)).Bytes()
	if errors.Is(err, redis.Nil) {
		return OtpRecord{}, false, nil
	}
	if err != nil {
		return OtpRecord{}, false, err
	}
	var rec OtpRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return OtpRecord{}, false, err
	}
	if !rec.ExpiresAt.After(now) {
		return OtpRecord{}, false, nil
	}
	return rec, true, nil
}

// SetOTP сохраняет запись OTP с TTL = остаток до rec.ExpiresAt (перезапись при
// инкременте попыток сохраняет исходное окно, не продлевая его).
func (s *RedisOtpStore) SetOTP(ctx context.Context, phone string, rec OtpRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	ttl := time.Until(rec.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Second // истёкшую запись не храним дольше, чем нужно
	}
	return s.rdb.Set(ctx, otpKey(phone), body, ttl).Err()
}

// DelOTP удаляет запись OTP (потребление после успешной верификации).
func (s *RedisOtpStore) DelOTP(ctx context.Context, phone string) error {
	return s.rdb.Del(ctx, otpKey(phone)).Err()
}

// IsBlocked сообщает наличие ключа blocked:{phone}.
func (s *RedisOtpStore) IsBlocked(ctx context.Context, phone string, _ time.Time) (bool, error) {
	n, err := s.rdb.Exists(ctx, otpBlockKey(phone)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Block ставит blocked:{phone} с TTL = остаток до until (OtpBlockTTL).
func (s *RedisOtpStore) Block(ctx context.Context, phone string, until time.Time) error {
	ttl := time.Until(until)
	if ttl <= 0 {
		return nil
	}
	return s.rdb.Set(ctx, otpBlockKey(phone), "1", ttl).Err()
}

// RedisRefreshKV — боевая реализация refreshKV поверх go-redis (ключи строит
// RefreshWhitelist: auth:refresh:{jti} → user_id, TTL RefreshTTL).
type RedisRefreshKV struct {
	rdb *redis.Client
}

// NewRedisRefreshKV собирает KV whitelist refresh-токенов.
func NewRedisRefreshKV(rdb *redis.Client) *RedisRefreshKV {
	return &RedisRefreshKV{rdb: rdb}
}

// Set сохраняет key→val с TTL.
func (s *RedisRefreshKV) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	return s.rdb.Set(ctx, key, val, ttl).Err()
}

// Get возвращает val; отсутствие ключа → ok=false (истёк/отозван/ротирован).
func (s *RedisRefreshKV) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := s.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Del удаляет key (идемпотентно).
func (s *RedisRefreshKV) Del(ctx context.Context, key string) error {
	return s.rdb.Del(ctx, key).Err()
}
