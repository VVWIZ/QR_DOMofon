// Package logging конфигурирует структурированный slog JSON-логгер (stdout).
// Уровень берётся из окружения; поле request_id добавляется per-request в
// platform/httpx (middleware) через logger.With.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New создаёт JSON-логгер stdout с уровнем level (debug|info|warn|error).
// Неизвестный уровень → info.
func New(level string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(level)})
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger
}

// parseLevel преобразует строковый уровень в slog.Level (дефолт — info).
func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
