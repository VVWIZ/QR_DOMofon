package httpx

import (
	"net/http"
	"time"

	"github.com/go-chi/httprate"
)

// RateLimit — ограничение частоты по IP (prod-гэп §5.6, §17.3): не более requests
// запросов за window с одного IP. Превышение → 429 RATE_LIMIT в едином конверте
// ошибок. Реализация in-memory (достаточно для skeleton/одного инстанса); в проде —
// Redis-backed httprate или ограничение на API Gateway, плюс ключи по public_id
// и session-token (ТЗ §5.6).
func RateLimit(requests int, window time.Duration) func(http.Handler) http.Handler {
	return httprate.Limit(
		requests,
		window,
		httprate.WithKeyByIP(),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, CodeRateLimit, "Too many requests, slow down", RequestIDFromContext(r.Context()))
		}),
	)
}
