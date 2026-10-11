package apierr

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"scibox/server/internal/textsafe"
)

// errTrailingData — после документа в теле есть что-то ещё.
var errTrailingData = errors.New("apierr: data after the JSON document")

// CodeUnsupportedMedia — запрос не в формате JSON.
const CodeUnsupportedMedia = "unsupported_media_type"

// DecodeJSON читает тело запроса как JSON в dst. Принимает только Content-Type: application/json,
// не больше maxBytes, без неизвестных полей и без данных после документа. При ошибке сам отвечает по формату API и возвращает false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "Запрос должен быть в формате JSON")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// Второй документ или мусор после первого: разбирать «наполовину» нельзя, ни одна форма сайта так не пишет.
		var rest json.RawMessage
		if next := dec.Decode(&rest); !errors.Is(next, io.EOF) {
			err = errTrailingData
			if _, tooBig := errors.AsType[*http.MaxBytesError](next); tooBig {
				err = next
			}
		}
	}
	if err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			WriteError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Запрос слишком большой")
			return false
		}
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "Не удалось прочитать запрос")
		return false
	}
	// Нулевой знак (\u0000) JSON разбирает без ошибки, а база его не принимает: до обработчика такой ввод не доходит.
	if !textsafe.ValidValue(dst) {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "В запросе есть недопустимые знаки")
		return false
	}
	return true
}
