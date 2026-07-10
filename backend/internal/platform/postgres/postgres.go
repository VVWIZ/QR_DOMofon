// Package postgres — pgxpool-подключение и автоприменение goose-миграций при
// старте (architecture.md §4.7, идемпотентно). Миграции встроены в бинарь
// (backend/migrations), сюда приходят как fs.FS — пакет остаётся инфраструктурно
// нейтральным и не импортирует прикладной пакет миграций.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql драйвер "pgx" для goose
	"github.com/pressly/goose/v3"
)

// Connect открывает пул pgx и проверяет доступность БД пингом.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: new pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

// Migrate применяет все goose-миграции из fsys к БД по dsn (goose dialect
// postgres, через database/sql драйвер pgx). Идемпотентно: уже применённые
// версии пропускаются.
func Migrate(dsn string, fsys fs.FS) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("postgres: open sql: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(fsys)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("postgres: goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("postgres: goose up: %w", err)
	}
	return nil
}
