// Package calls — сессии звонков (Redis), комнаты/токены LiveKit и события
// жильцу через интерфейс Notifier (architecture.md §1, §4.2–4.3). Владелец
// эфемерного состояния звонка; в БД звонки не хранятся.
package calls

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// callTTL — TTL call-сессии (авто-очистка зависших звонков, AC13/§4.2).
const callTTL = 120 * time.Second

// Session — детали звонка (эфемерные, в Redis под ключом call:{id}).
type Session struct {
	CallID              string `json:"call_id"`
	ApartmentID         string `json:"apartment_id"`
	AccessPointID       string `json:"access_point_id"`
	AccessPointLabel    string `json:"access_point_label"`
	DeviceID            string `json:"device_id"`
	ManagementCompanyID string `json:"management_company_id"`
	State               string `json:"state"`
}

// apartmentBusyKey / callKey — ключи Redis (§4.2).
func apartmentBusyKey(apartmentID string) string { return "call:apartment:" + apartmentID }
func callKey(callID string) string               { return "call:" + callID }

// Store — Redis-хранилище call-сессий.
type Store struct {
	rdb *redis.Client
}

// NewStore создаёт хранилище сессий.
func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

// Create атомарно занимает квартиру (SET NX EX 120) и сохраняет детали звонка.
// ok=false без ошибки — квартира уже занята (409 CALL_IN_PROGRESS).
func (s *Store) Create(ctx context.Context, sess Session) (bool, error) {
	ok, err := s.rdb.SetNX(ctx, apartmentBusyKey(sess.ApartmentID), sess.CallID, callTTL).Result()
	if err != nil {
		return false, fmt.Errorf("calls: setnx busy: %w", err)
	}
	if !ok {
		return false, nil
	}

	body, err := json.Marshal(sess)
	if err != nil {
		_ = s.rdb.Del(ctx, apartmentBusyKey(sess.ApartmentID)).Err()
		return false, fmt.Errorf("calls: marshal session: %w", err)
	}
	if err := s.rdb.Set(ctx, callKey(sess.CallID), body, callTTL).Err(); err != nil {
		_ = s.rdb.Del(ctx, apartmentBusyKey(sess.ApartmentID)).Err()
		return false, fmt.Errorf("calls: set session: %w", err)
	}
	return true, nil
}

// Get возвращает сессию по call_id (ok=false — не найдена/истекла).
func (s *Store) Get(ctx context.Context, callID string) (Session, bool, error) {
	body, err := s.rdb.Get(ctx, callKey(callID)).Bytes()
	if err == redis.Nil {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("calls: get session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(body, &sess); err != nil {
		return Session{}, false, fmt.Errorf("calls: unmarshal session: %w", err)
	}
	return sess, true, nil
}

// Delete снимает и детали звонка, и busy-ключ квартиры (cancel/end, §4.2).
func (s *Store) Delete(ctx context.Context, sess Session) error {
	if err := s.rdb.Del(ctx, callKey(sess.CallID), apartmentBusyKey(sess.ApartmentID)).Err(); err != nil {
		return fmt.Errorf("calls: delete session: %w", err)
	}
	return nil
}
