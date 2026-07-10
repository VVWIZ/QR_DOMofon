// Package redis — тонкая обёртка над go-redis v9. Хранит эфемерное состояние:
// presence устройств (device:online:{id}, TTL 90с), call-сессии
// (call:apartment:{id}, call:{id}, TTL 120с) и контекст команд для корреляции
// command_ack (architecture.md §4.1–4.2).
package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client — псевдоним клиента go-redis, чтобы потребители зависели от этого
// пакета, а не напрямую от go-redis.
type Client = redis.Client

// Connect создаёт клиент go-redis и best-effort пингует брокер (недоступность
// на старте не фатальна — go-redis переподключается лениво; статус виден в
// /health). Ошибка пинга возвращается вторым значением для логирования.
func Connect(ctx context.Context, addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return client, err
	}
	return client, nil
}
