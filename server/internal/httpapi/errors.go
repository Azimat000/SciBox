package httpapi

import "scibox/server/internal/apierr"

// Формат ошибок живёт в apierr; здесь короткие имена для обработчиков этого пакета.
const (
	CodeNotFound            = apierr.CodeNotFound
	CodeMethodNotAllowed    = apierr.CodeMethodNotAllowed
	CodeInternal            = apierr.CodeInternal
	CodeDatabaseUnavailable = apierr.CodeDatabaseUnavailable
)

type (
	ErrorBody   = apierr.ErrorBody
	ErrorDetail = apierr.ErrorDetail
)

var (
	WriteError = apierr.WriteError
	WriteJSON  = apierr.WriteJSON
)
