package auth

import (
	"context"
	"net/http"
	"strings"

	"domofon/backend/internal/platform/httpx"
)

// claimsCtxKey — ключ claims в context запроса (после Authenticator).
type claimsCtxKey struct{}

// ClaimsFromContext возвращает claims текущего запроса (ok=false, если запрос
// не прошёл Authenticator).
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsCtxKey{}).(Claims)
	return c, ok
}

// withClaims кладёт claims в context (используется Authenticator).
func withClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsCtxKey{}, c)
}

// extractToken достаёт access-токен из запроса: сначала "Authorization: Bearer
// <access>", иначе (для SSE, EventSource не шлёт заголовки) — query "?token=".
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		const prefix = "Bearer "
		if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
			return strings.TrimSpace(h[len(prefix):])
		}
	}
	return r.URL.Query().Get("token")
}

// Authenticator валидирует access-токен и кладёт claims в context. Токен берётся
// из заголовка "Authorization: Bearer <access>", а для SSE (EventSource не шлёт
// заголовки) — из query "?token=<access>". Нет/невалиден/просрочен → 401
// UNAUTHORIZED, next не вызывается (auth.md §6). Verifier инъектируется.
func Authenticator(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
				return
			}
			claims, err := v.VerifyAccess(token)
			if err != nil {
				httpx.WriteError(w, httpx.CodeUnauthorized, "invalid access token", httpx.RequestIDFromContext(r.Context()))
				return
			}
			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
		})
	}
}

// RequireResident пропускает только kind ∈ {resident, owner}; иначе 403
// FORBIDDEN. Ставится ПОСЛЕ Authenticator (читает claims из context).
func RequireResident(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFromContext(r.Context())
		if !ok || !c.Kind.IsResident() {
			httpx.WriteError(w, httpx.CodeForbidden, "resident role required", httpx.RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin пропускает только kind = mc_admin; иначе 403 FORBIDDEN. Ставится
// ПОСЛЕ Authenticator. system_admin сюда НЕ проходит (см. Kind.IsAdmin).
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFromContext(r.Context())
		if !ok || !c.Kind.IsAdmin() {
			httpx.WriteError(w, httpx.CodeForbidden, "admin role required", httpx.RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSystemAdmin пропускает только kind = system_admin (платформенная
// админка); иначе 403 FORBIDDEN. Ставится ПОСЛЕ Authenticator.
func RequireSystemAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFromContext(r.Context())
		if !ok || !c.Kind.IsSystemAdmin() {
			httpx.WriteError(w, httpx.CodeForbidden, "system admin role required", httpx.RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}
