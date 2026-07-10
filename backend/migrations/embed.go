// Package migrations встраивает SQL-миграции goose в бинарь, чтобы cmd/server
// применял их автоматически при старте без внешних файлов (architecture.md §4.7).
package migrations

import "embed"

// FS — встроенная файловая система с миграциями (*.sql в этом каталоге).
// Передаётся в platform/postgres.Migrate → goose.SetBaseFS.
//
//go:embed *.sql
var FS embed.FS
