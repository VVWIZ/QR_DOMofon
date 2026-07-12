package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubVerifier изолирует middleware от RSA: возвращает заранее заданные claims
// для известного токена, ошибку — для остальных.
type stubVerifier struct {
	token  string
	claims Claims
}

func (s stubVerifier) VerifyAccess(token string) (Claims, error) {
	if token == s.token {
		return s.claims, nil
	}
	return Claims{}, http.ErrNoCookie // произвольная ошибка «невалидный токен»
}

func residentVerifier(token string) stubVerifier {
	return stubVerifier{token: token, claims: residentClaims()}
}

func adminVerifier(token string) stubVerifier {
	return stubVerifier{token: token, claims: adminClaims()}
}

// okHandler фиксирует факт вызова next и наличие claims в context.
type okHandler struct {
	called bool
	claims Claims
	hasCl  bool
}

func (h *okHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	h.called = true
	h.claims, h.hasCl = ClaimsFromContext(r.Context())
}

func TestAuthenticator_NoTokenUnauthorized(t *testing.T) {
	next := &okHandler{}
	h := Authenticator(residentVerifier("good-token"))(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("статус = %d, want 401", rec.Code)
	}
	if next.called {
		t.Fatalf("next вызван без токена, want не вызван")
	}
}

func TestAuthenticator_ValidBearerCallsNextWithClaims(t *testing.T) {
	const token = "good-token"
	next := &okHandler{}
	h := Authenticator(residentVerifier(token))(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("next не вызван при валидном токене")
	}
	if !next.hasCl {
		t.Fatalf("claims не проброшены в context")
	}
	if next.claims.Subject != residentClaims().Subject {
		t.Fatalf("claims.Subject = %q, want %q", next.claims.Subject, residentClaims().Subject)
	}
}

func TestAuthenticator_SSETokenInQuery(t *testing.T) {
	const token = "sse-token"
	next := &okHandler{}
	h := Authenticator(residentVerifier(token))(next)

	rec := httptest.NewRecorder()
	// EventSource не шлёт заголовки → токен в ?token=.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/resident/events?token="+token, nil)
	h.ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("next не вызван при токене в query (?token=)")
	}
}

func TestRequireAdmin_ResidentForbidden(t *testing.T) {
	const token = "resident-token"
	next := &okHandler{}
	// Authenticator(resident) → RequireAdmin: роли недостаточно → 403.
	h := Authenticator(residentVerifier(token))(RequireAdmin(next))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("статус = %d, want 403", rec.Code)
	}
	if next.called {
		t.Fatalf("next вызван для resident на RequireAdmin, want не вызван")
	}
}

func TestRequireAdmin_AdminAllowed(t *testing.T) {
	const token = "admin-token"
	next := &okHandler{}
	h := Authenticator(adminVerifier(token))(RequireAdmin(next))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("next не вызван для mc_admin на RequireAdmin")
	}
}

func TestRequireResident_ResidentAllowed(t *testing.T) {
	const token = "resident-token"
	next := &okHandler{}
	h := Authenticator(residentVerifier(token))(RequireResident(next))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/access/open", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("next не вызван для resident на RequireResident")
	}
}
