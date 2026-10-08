package httpapi

import "scibox/server/internal/apierr"

// Формат ошибок живёт в apierr; здесь короткие имена для обработчиков этого пакета.
const (
	CodeNotFound            = apierr.CodeNotFound
	CodeMethodNotAllowed    = apierr.CodeMethodNotAllowed
	CodeInternal            = apierr.CodeInternal
	CodeDatabaseUnavailable = apierr.CodeDatabaseUnavailable
)

// Общий формат ошибок из apierr: обработчики пакета httpapi пишут ответы тем же форматом.
type (
	// ErrorBody — тело ответа с ошибкой.
	ErrorBody = apierr.ErrorBody
	// ErrorDetail — сама ошибка: код, сообщение, поля.
	ErrorDetail = apierr.ErrorDetail
)

// WriteError и WriteJSON — те же функции, что в apierr.
var (
	WriteError = apierr.WriteError
	WriteJSON  = apierr.WriteJSON
)
