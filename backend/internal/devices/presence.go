// Package devices — реестр устройств, presence в Redis, публикация команд
// open_relay и разбор входящих статусов (heartbeat/command_ack/event). Статус
// online/offline — производное от Redis-presence, не колонка БД
// (architecture.md §4.1).
package devices

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// presenceTTL — время жизни presence-ключа устройства (порог offline, AC9).
const presenceTTL = 90 * time.Second

// presenceKey формирует Redis-ключ presence устройства.
func presenceKey(deviceID string) string { return "device:online:" + deviceID }

// Presence — presence устройств поверх Redis (SET EX 90 на каждый heartbeat).
// Реализует потребительские интерфейсы IsOnline в qr/calls/access (структурная
// типизация — адаптеры не нужны).
type Presence struct {
	rdb *redis.Client
}

// NewPresence создаёт presence-хранилище.
func NewPresence(rdb *redis.Client) *Presence {
	return &Presence{rdb: rdb}
}

// Mark отмечает устройство online: SET device:online:{id} EX 90 (на heartbeat).
func (p *Presence) Mark(ctx context.Context, deviceID string) error {
	return p.rdb.Set(ctx, presenceKey(deviceID), "1", presenceTTL).Err()
}

// IsOnline сообщает наличие presence-ключа устройства (нет ключа = offline).
func (p *Presence) IsOnline(ctx context.Context, deviceID string) (bool, error) {
	n, err := p.rdb.Exists(ctx, presenceKey(deviceID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
