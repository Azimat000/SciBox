package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Машинные коды ошибок API (D-032). Тексты для людей лежат рядом, по-русски.
const (
	CodeNotFound            = "not_found"
	CodeMethodNotAllowed    = "method_not_allowed"
	CodeInternal            = "internal"
	CodeDatabaseUnavailable = "database_unavailable"
)

// ErrorBody — единый формат ошибки: {"error": {"code": "...", "message": "..."}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail — код и сообщение ошибки.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError отвечает ошибкой в едином формате.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// WriteJSON отвечает JSON с указанным статусом.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "err", err)
	}
}
