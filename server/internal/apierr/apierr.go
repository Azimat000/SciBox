// Package apierr — единый формат ответов API и ошибок (D-032).
// Вынесен отдельно, чтобы им могли пользоваться и httpapi, и пакеты разделов (auth и другие).
package apierr

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Машинные коды ошибок API. Тексты для людей лежат рядом, по-русски.
const (
	CodeNotFound            = "not_found"
	CodeMethodNotAllowed    = "method_not_allowed"
	CodeInternal            = "internal"
	CodeDatabaseUnavailable = "database_unavailable"
	CodeBadRequest          = "bad_request"
	CodeValidation          = "validation_failed"
	CodeUnauthorized        = "unauthorized"
	CodeForbidden           = "forbidden"
	CodeRateLimited         = "rate_limited"
)

// ErrorBody — единый формат ошибки: {"error": {"code": "...", "message": "..."}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail — код и сообщение ошибки. Fields заполняется для ошибок проверки формы:
// ключ — имя поля, значение — что с ним не так.
type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	// RetryAfter — через сколько секунд можно повторить (для кода rate_limited).
	RetryAfter int `json:"retry_after,omitempty"`
}

// WriteError отвечает ошибкой в едином формате.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// WriteFieldErrors отвечает 422 с описанием, что не так в каждом поле формы.
func WriteFieldErrors(w http.ResponseWriter, message string, fields map[string]string) {
	WriteJSON(w, http.StatusUnprocessableEntity, ErrorBody{Error: ErrorDetail{Code: CodeValidation, Message: message, Fields: fields}})
}

// WriteJSON отвечает JSON с указанным статусом.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "err", err)
	}
}

// StatusClientClosed — запрос оборвал сам человек: ушёл со страницы, закрыл вкладку.
// Код взят из nginx (499); отвечать уже некому, он нужен только в журнале запросов.
const StatusClientClosed = 499

// WriteInternal отвечает 500 на непредвиденную ошибку и пишет её в журнал.
// Если запрос оборвал сам человек, это не поломка: в журнал идёт спокойная строка, а не ошибка,
// чтобы настоящие сбои не терялись среди обрывов.
func WriteInternal(w http.ResponseWriter, r *http.Request, logger *slog.Logger, what string, err error) {
	if r.Context().Err() != nil {
		logger.Info(what+" cancelled by client", "path", r.URL.Path)
		w.WriteHeader(StatusClientClosed)
		return
	}
	logger.Error(what+" request failed", "path", r.URL.Path, "err", err)
	WriteError(w, http.StatusInternalServerError, CodeInternal, "Что-то сломалось на сервере. Попробуйте ещё раз")
}
