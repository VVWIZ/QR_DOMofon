package qr

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgKeyring — реализация Keyring поверх таблицы qr_keys (только is_active).
// Активные ключи загружаются в память при старте: интерфейс Keyring.Secret не
// принимает context и не возвращает error (юнит-контракт qr_test.go), поэтому
// синхронный map-lookup — самый чистый способ его удовлетворить. Ротация ключа —
// перезапуск/Refresh (в skeleton ключей единицы, меняются редко).
type PgKeyring struct {
	secrets map[string]string
}

// NewKeyring загружает все активные ключи (kid → secret) из qr_keys.
func NewKeyring(ctx context.Context, pool *pgxpool.Pool) (*PgKeyring, error) {
	k := &PgKeyring{secrets: make(map[string]string)}
	if err := k.load(ctx, pool); err != nil {
		return nil, err
	}
	return k, nil
}

// load читает активные ключи из БД в память.
func (k *PgKeyring) load(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT kid, secret FROM qr_keys WHERE is_active = true`)
	if err != nil {
		return fmt.Errorf("qr: load keys: %w", err)
	}
	defer rows.Close()

	secrets := make(map[string]string)
	for rows.Next() {
		var kid, secret string
		if err := rows.Scan(&kid, &secret); err != nil {
			return fmt.Errorf("qr: scan key: %w", err)
		}
		secrets[kid] = secret
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("qr: rows: %w", err)
	}
	k.secrets = secrets
	return nil
}

// Secret возвращает секрет для kid; ok=false, если kid неизвестен/неактивен.
func (k *PgKeyring) Secret(kid string) (string, bool) {
	s, ok := k.secrets[kid]
	return s, ok
}
