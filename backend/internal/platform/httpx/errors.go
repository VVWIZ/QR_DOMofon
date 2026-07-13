// Package httpx — инфраструктурный HTTP-слой (роутер, SSE, единый конверт ошибок).
//
// Этот файл фиксирует контракт ошибочного ответа API (api.md "Формат ошибок",
// ТЗ §13.1): единый JSON-конверт {"error":{code,message,request_id}} и маппинг
// доменных кодов на HTTP-статусы.
//
// Реализовано на этапе backend под контракт, покрытый тестами errors_test.go.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Code — доменный код ошибки API (значение поля error.code в конверте).
type Code string

// Реестр кодов ошибок (api.md "Формат ошибок").
const (
	CodeInvalidQR       Code = "INVALID_QR"
	CodeValidationError Code = "VALIDATION_ERROR"
	CodeCallNotFound    Code = "CALL_NOT_FOUND"
	CodeCallNotAccepted Code = "CALL_NOT_ACCEPTED"
	CodeCallInProgress  Code = "CALL_IN_PROGRESS"
	CodeDeviceOffline   Code = "DEVICE_OFFLINE"
	CodeRateLimit       Code = "RATE_LIMIT"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeInternal        Code = "INTERNAL"

	// Инкремент онбординга (онбординг + гранты). Инвайт-токен по одноразовой
	// ссылке: не найден/уже использован → INVITE_INVALID (404), просрочен →
	// INVITE_EXPIRED (410 Gone — ресурс существовал, но истёк).
	CodeInviteInvalid Code = "INVITE_INVALID"
	CodeInviteExpired Code = "INVITE_EXPIRED"
)

// ErrorResponse — единый конверт ошибки (сериализуется в тело ответа).
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody — содержимое конверта ошибки.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// HTTPStatus возвращает HTTP-статус для доменного кода:
//
//	INVALID_QR, VALIDATION_ERROR      → 400
//	CALL_NOT_FOUND, INVITE_INVALID    → 404
//	INVITE_EXPIRED                    → 410
//	CALL_NOT_ACCEPTED, CALL_IN_PROGRESS → 409
//	UNAUTHORIZED                      → 401
//	FORBIDDEN                         → 403
//	RATE_LIMIT                        → 429
//	DEVICE_OFFLINE                    → 503
//	INTERNAL                          → 500
//
// Неизвестный/пустой код → 500 (INTERNAL) как безопасный дефолт.
func HTTPStatus(code Code) int {
	switch code {
	case CodeInvalidQR, CodeValidationError:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeCallNotFound, CodeInviteInvalid:
		return http.StatusNotFound
	case CodeInviteExpired:
		return http.StatusGone
	case CodeCallNotAccepted, CodeCallInProgress:
		return http.StatusConflict
	case CodeRateLimit:
		return http.StatusTooManyRequests
	case CodeDeviceOffline:
		return http.StatusServiceUnavailable
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// WriteError пишет в w единый конверт ошибки:
//
//   - HTTP-статус = HTTPStatus(code);
//   - заголовок Content-Type: application/json;
//   - тело = {"error":{"code","message","request_id"}}.
func WriteError(w http.ResponseWriter, code Code, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(HTTPStatus(code))
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:      string(code),
			Message:   message,
			RequestID: requestID,
		},
	})
}
