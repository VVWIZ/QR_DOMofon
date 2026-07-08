// Package httpx — инфраструктурный HTTP-слой (роутер, SSE, единый конверт ошибок).
//
// Этот файл фиксирует контракт ошибочного ответа API (api.md "Формат ошибок",
// ТЗ §13.1): единый JSON-конверт {"error":{code,message,request_id}} и маппинг
// доменных кодов на HTTP-статусы.
//
// СКЕЛЕТ ЭТАПА QA: тела функций паникуют ("not implemented"). Реализацию пишет
// этап backend — здесь только контракт под RED-тесты.
package httpx

import "net/http"

// Code — доменный код ошибки API (значение поля error.code в конверте).
type Code string

// Реестр кодов ошибок (api.md "Формат ошибок").
const (
	CodeInvalidQR       Code = "INVALID_QR"
	CodeValidationError Code = "VALIDATION_ERROR"
	CodeCallNotFound    Code = "CALL_NOT_FOUND"
	CodeCallInProgress  Code = "CALL_IN_PROGRESS"
	CodeDeviceOffline   Code = "DEVICE_OFFLINE"
	CodeInternal        Code = "INTERNAL"
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
//	INVALID_QR, VALIDATION_ERROR → 400
//	CALL_NOT_FOUND               → 404
//	CALL_IN_PROGRESS             → 409
//	DEVICE_OFFLINE               → 503
//	INTERNAL                     → 500
//
// Неизвестный/пустой код → 500 (INTERNAL) как безопасный дефолт.
func HTTPStatus(code Code) int {
	panic("not implemented: httpx.HTTPStatus")
}

// WriteError пишет в w единый конверт ошибки:
//
//   - HTTP-статус = HTTPStatus(code);
//   - заголовок Content-Type: application/json;
//   - тело = {"error":{"code","message","request_id"}}.
func WriteError(w http.ResponseWriter, code Code, message, requestID string) {
	panic("not implemented: httpx.WriteError")
}
