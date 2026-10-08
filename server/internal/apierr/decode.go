package apierr

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
)

// CodeUnsupportedMedia — запрос не в формате JSON.
const CodeUnsupportedMedia = "unsupported_media_type"

// DecodeJSON читает тело запроса как JSON в dst. Принимает только Content-Type: application/json,
// не больше maxBytes и без неизвестных полей. При ошибке сам отвечает по формату API и возвращает false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "Запрос должен быть в формате JSON")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			WriteError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Запрос слишком большой")
			return false
		}
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "Не удалось прочитать запрос")
		return false
	}
	return true
}
