package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON пишет v как JSON с указанным HTTP-статусом.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error — доменная ошибка сервиса с кодом из реестра (errors.go) и сообщением
// для клиента. Сервисы возвращают *Error, хендлеры отдают его через WriteErr —
// так конверт ошибки формируется единообразно (ТЗ §13.1).
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// NewError конструирует доменную ошибку.
func NewError(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WriteErr пишет *Error как единый конверт, подставляя request_id из контекста.
func WriteErr(w http.ResponseWriter, r *http.Request, e *Error) {
	WriteError(w, e.Code, e.Message, RequestIDFromContext(r.Context()))
}
