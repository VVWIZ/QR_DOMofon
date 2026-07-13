package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPStatus_Mapping(t *testing.T) {
	cases := map[Code]int{
		CodeInvalidQR:       http.StatusBadRequest,          // 400
		CodeValidationError: http.StatusBadRequest,          // 400
		CodeCallNotFound:    http.StatusNotFound,            // 404
		CodeCallInProgress:  http.StatusConflict,            // 409
		CodeDeviceOffline:   http.StatusServiceUnavailable,  // 503
		CodeInternal:        http.StatusInternalServerError, // 500
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Errorf("HTTPStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestHTTPStatus_AuthCodes(t *testing.T) {
	// Инкремент auth/RBAC (auth.md §6): UNAUTHORIZED→401, FORBIDDEN→403.
	cases := map[Code]int{
		CodeUnauthorized: http.StatusUnauthorized,    // 401
		CodeForbidden:    http.StatusForbidden,       // 403
		CodeRateLimit:    http.StatusTooManyRequests, // 429
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Errorf("HTTPStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestHTTPStatus_InviteCodes(t *testing.T) {
	// Инкремент онбординга: INVITE_INVALID→404 (не найден/использован),
	// INVITE_EXPIRED→410 Gone (ссылка истекла).
	cases := map[Code]int{
		CodeInviteInvalid: http.StatusNotFound, // 404
		CodeInviteExpired: http.StatusGone,     // 410
	}
	for code, want := range cases {
		if got := HTTPStatus(code); got != want {
			t.Errorf("HTTPStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestHTTPStatus_UnknownCodeDefaults500(t *testing.T) {
	if got := HTTPStatus(Code("SOMETHING_ELSE")); got != http.StatusInternalServerError {
		t.Fatalf("HTTPStatus(неизвестный код) = %d, want 500", got)
	}
}

func TestWriteError_Envelope(t *testing.T) {
	rec := httptest.NewRecorder()
	const (
		msg = "Device is offline, door cannot be opened remotely"
		rid = "9c1b7a3e-5f20-4d55-8b1a-2e6f0c4d7a91"
	)
	WriteError(rec, CodeDeviceOffline, msg, rid)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("статус = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want префикс application/json", ct)
	}

	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не является валидным JSON: %v (body=%q)", err, rec.Body.String())
	}
	if got.Error.Code != string(CodeDeviceOffline) {
		t.Errorf("error.code = %q, want %q", got.Error.Code, CodeDeviceOffline)
	}
	if got.Error.Message != msg {
		t.Errorf("error.message = %q, want %q", got.Error.Message, msg)
	}
	if got.Error.RequestID != rid {
		t.Errorf("error.request_id = %q, want %q", got.Error.RequestID, rid)
	}
}

func TestWriteError_EnvelopeShape(t *testing.T) {
	// Точная форма конверта (api.md §13.1): верхний ключ "error" с вложенными
	// полями "code" / "message" / "request_id".
	rec := httptest.NewRecorder()
	WriteError(rec, CodeInvalidQR, "bad qr", "rid-1")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("невалидный JSON: %v", err)
	}
	inner, ok := raw["error"]
	if !ok {
		t.Fatalf("нет верхнего ключа \"error\", body=%q", rec.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(inner, &fields); err != nil {
		t.Fatalf("error не является объектом: %v", err)
	}
	for _, key := range []string{"code", "message", "request_id"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("в объекте error отсутствует поле %q", key)
		}
	}
}
