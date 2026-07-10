package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// NewRouter создаёт chi-роутер с базовыми middleware (request_id → access-log →
// recover). Монтирование модулей (/health, /api/v1/...) делает composition root
// (cmd/server), чтобы роутер оставался инфраструктурно нейтральным.
func NewRouter(log *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(RequestID(log))
	r.Use(AccessLog(log))
	r.Use(Recover(log))
	return r
}

// HealthFunc — проверка одной зависимости (nil = ok).
type HealthFunc func(ctx context.Context) error

// Health отдаёт {status, deps}: status=ok если все зависимости живы, иначе
// degraded (api.md GET /health). Каждая проверка ограничена таймаутом.
func Health(checks map[string]HealthFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deps := make(map[string]string, len(checks))
		status := "ok"
		for name, fn := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			err := fn(ctx)
			cancel()
			if err != nil {
				deps[name] = "error"
				status = "degraded"
				continue
			}
			deps[name] = "ok"
		}
		WriteJSON(w, http.StatusOK, map[string]any{"status": status, "deps": deps})
	}
}
