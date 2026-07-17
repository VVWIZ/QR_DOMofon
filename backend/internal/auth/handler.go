package auth

// HTTP-хендлеры auth (api.md "Аутентификация"): тела/ответы/коды, refresh-cookie
// (HttpOnly, Secure, SameSite=Strict, Path=/api/v1/auth, Max-Age 30д). Единый
// конверт ошибок — через httpx.

import (
	"encoding/json"
	"net/http"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// Параметры refresh-cookie (api.md, auth.md §3).
const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

// Handler обслуживает /api/v1/auth/*.
type Handler struct {
	svc *Service
}

// NewHandler создаёт хендлер auth.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- Тела запросов ---

type otpSendRequest struct {
	Phone string `json:"phone"`
}

type otpVerifyRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

type adminLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

// --- Тела ответов ---

type otpSendResponse struct {
	Sent    bool   `json:"sent"`
	DevCode string `json:"dev_code,omitempty"`
}

type apartmentResponse struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type userResponse struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"`
	Apartments []apartmentResponse `json:"apartments"`
	MCID       *string             `json:"mc_id"`
}

type loginResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   int          `json:"expires_in"`
	User        userResponse `json:"user"`
}

type refreshResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// OtpSend — POST /api/v1/auth/otp/send.
func (h *Handler) OtpSend(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	var req otpSendRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.Phone == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Field phone is required", rid)
		return
	}

	res, apiErr := h.svc.OtpSend(r.Context(), req.Phone)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, otpSendResponse{Sent: res.Sent, DevCode: res.DevCode})
}

// OtpVerify — POST /api/v1/auth/otp/verify.
func (h *Handler) OtpVerify(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	var req otpVerifyRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	if req.Phone == "" || req.Code == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields phone and code are required", rid)
		return
	}

	res, apiErr := h.svc.OtpVerify(r.Context(), req.Phone, req.Code)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	setRefreshCookie(w, res.RefreshToken)
	httpx.WriteJSON(w, http.StatusOK, loginBody(res))
}

// AdminLogin — POST /api/v1/auth/admin/login.
func (h *Handler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	rid := httpx.RequestIDFromContext(r.Context())
	var req adminLoginRequest
	if !decodeBody(w, r, &req, rid) {
		return
	}
	// totp_code обязателен по факту только вне dev-режима (проверка 2FA — в сервисе,
	// пропускается при AUTH_DEV_MODE). Здесь требуем только email+пароль.
	if req.Email == "" || req.Password == "" {
		httpx.WriteError(w, httpx.CodeValidationError, "Fields email and password are required", rid)
		return
	}

	res, apiErr := h.svc.AdminLogin(r.Context(), req.Email, req.Password, req.TOTPCode)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	setRefreshCookie(w, res.RefreshToken)
	httpx.WriteJSON(w, http.StatusOK, loginBody(res))
}

// Refresh — POST /api/v1/auth/refresh (refresh из cookie).
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	token := refreshCookieValue(r)
	res, apiErr := h.svc.Refresh(r.Context(), token)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	setRefreshCookie(w, res.RefreshToken)
	httpx.WriteJSON(w, http.StatusOK, refreshResponse{
		AccessToken: res.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(AccessTTL / time.Second),
	})
}

// Logout — POST /api/v1/auth/logout (204, идемпотентно).
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.svc.Logout(r.Context(), refreshCookieValue(r))
	clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me — GET /api/v1/auth/me (требует Authenticator: claims уже в контексте).
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeUnauthorized, "missing access token", httpx.RequestIDFromContext(r.Context()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userBody(h.svc.Me(claims)))
}

// WriteLogin ставит refresh-cookie и пишет тело логина (access-токен + профиль,
// 200). Публичный хелпер для онбординга: приём инвайта = вход без OTP, тот же
// контракт ответа, что otp/verify и admin/login.
func WriteLogin(w http.ResponseWriter, res LoginResult) {
	setRefreshCookie(w, res.RefreshToken)
	httpx.WriteJSON(w, http.StatusOK, loginBody(res))
}

// --- Вспомогательное ---

// decodeBody читает JSON-тело (лимит 64 KiB); при ошибке пишет VALIDATION_ERROR
// и возвращает false.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, rid string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, httpx.CodeValidationError, "Invalid request body", rid)
		return false
	}
	return true
}

// loginBody собирает тело ответа otp/verify и admin/login.
func loginBody(res LoginResult) loginResponse {
	return loginResponse{
		AccessToken: res.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(AccessTTL / time.Second),
		User:        userBody(res.User),
	}
}

// userBody конвертирует UserProfile в JSON-форму (mc_id: "" → null).
func userBody(p UserProfile) userResponse {
	apts := make([]apartmentResponse, 0, len(p.Apartments))
	for _, a := range p.Apartments {
		apts = append(apts, apartmentResponse{ID: a.ID, Role: a.Role})
	}
	return userResponse{
		ID:         p.ID,
		Kind:       string(p.Kind),
		Apartments: apts,
		MCID:       mcPtr(p.MCID),
	}
}

// mcPtr: пустая строка → nil (JSON null), иначе указатель на значение.
func mcPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// refreshCookieValue достаёт refresh-токен из cookie ("" если нет).
func refreshCookieValue(r *http.Request) string {
	c, err := r.Cookie(refreshCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// setRefreshCookie ставит refresh-cookie (HttpOnly, Secure, SameSite=Strict,
// Path=/api/v1/auth, Max-Age = RefreshTTL).
func setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(RefreshTTL / time.Second),
	})
}

// clearRefreshCookie очищает refresh-cookie (Max-Age=0).
func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
