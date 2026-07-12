package auth

import (
	"context"
	"net/http"
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

// Authenticator валидирует access-токен и кладёт claims в context. Токен берётся
// из заголовка "Authorization: Bearer <access>", а для SSE (EventSource не шлёт
// заголовки) — из query "?token=<access>". Нет/невалиден/просрочен → 401
// UNAUTHORIZED, next не вызывается (auth.md §6). Verifier инъектируется.
func Authenticator(v Verifier) func(http.Handler) http.Handler {
	panic("not implemented: auth.Authenticator")
}

// RequireResident пропускает только kind ∈ {resident, owner}; иначе 403
// FORBIDDEN. Ставится ПОСЛЕ Authenticator (читает claims из context).
func RequireResident(next http.Handler) http.Handler {
	panic("not implemented: auth.RequireResident")
}

// RequireAdmin пропускает только kind = mc_admin; иначе 403 FORBIDDEN. Ставится
// ПОСЛЕ Authenticator.
func RequireAdmin(next http.Handler) http.Handler {
	panic("not implemented: auth.RequireAdmin")
}
