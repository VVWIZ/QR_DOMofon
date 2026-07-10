package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestIDHeader — заголовок корреляции HTTP-запроса с логами.
const RequestIDHeader = "X-Request-ID"

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxLogger
)

// RequestIDFromContext возвращает request_id текущего запроса ("" если нет).
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// LoggerFromContext возвращает логгер запроса с полем request_id (или дефолтный).
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// RequestID проставляет request_id (из заголовка или сгенерированный uuid) в
// контекст, response-заголовок и логгер запроса.
func RequestID(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(RequestIDHeader, id)

			ctx := context.WithValue(r.Context(), ctxRequestID, id)
			ctx = context.WithValue(ctx, ctxLogger, base.With("request_id", id))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AccessLog логирует каждый запрос (метод, путь, статус, длительность) через slog.
func AccessLog(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			LoggerFromContext(r.Context()).Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// Recover перехватывает панику в хендлере, логирует и отдаёт 500 INTERNAL.
func Recover(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					LoggerFromContext(r.Context()).Error("http_panic", "panic", rec, "path", r.URL.Path)
					WriteError(w, CodeInternal, "Internal server error", RequestIDFromContext(r.Context()))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder перехватывает код ответа и пробрасывает Flush (нужно для SSE).
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Flush пробрасывает http.Flusher нижележащего writer'а (SSE-поток жильца).
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
