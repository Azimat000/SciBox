package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"scibox/server/internal/apierr"
)

// Коды ошибок этого раздела.
const (
	CodeBodyTooBig = "payload_too_large"
)

const (
	// partData — часть запроса с JSON, partFile — части с файлами.
	partData = "data"
	partFile = "file"
	// bodyOverhead — запас на служебные заголовки частей multipart.
	bodyOverhead = 64 << 10
)

// Form — разобранный запрос: JSON из части data и файлы из частей file в порядке следования.
type Form struct {
	Data    []byte
	Uploads []Upload
}

// ReadForm читает multipart-запрос: часть `data` (JSON не больше maxData байт) и до maxFiles частей `file`.
// Каждый файл читается с ограничением размера, весь запрос тоже ограничен, поэтому большой файл не съест память.
// Здесь проверяется только форма запроса; что файлы — настоящие PDF, проверяют Check и CheckSet.
func ReadForm(w http.ResponseWriter, r *http.Request, maxData int64, maxFiles int) (Form, error) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		return Form{}, ErrBadForm
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxData+int64(maxFiles)*MaxFileSize+bodyOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return Form{}, ErrBadForm
	}
	var form Form
	haveData := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Form{}, bodyErr(err)
		}
		switch part.FormName() {
		case partData:
			if haveData {
				return Form{}, ErrBadForm
			}
			haveData = true
			if form.Data, err = readLimited(part, maxData); err != nil {
				return Form{}, err
			}
		case partFile:
			if len(form.Uploads) >= maxFiles {
				return Form{}, ErrTooMany
			}
			data, err := readLimited(part, MaxFileSize)
			if err != nil {
				return Form{}, wrapFile(part, err)
			}
			form.Uploads = append(form.Uploads, Upload{Name: part.FileName(), Data: data})
		default:
			return Form{}, ErrBadForm
		}
	}
	if !haveData {
		return Form{}, ErrBadForm
	}
	return form, nil
}

// wrapFile приписывает ошибку файлу, чтобы человеку можно было назвать его по имени.
func wrapFile(p *multipart.Part, err error) error {
	if errors.Is(err, ErrTooBig) {
		return &FileError{Name: CleanName(p.FileName()), Err: err}
	}
	return err
}

// readLimited читает не больше limit байт; если данных больше, возвращает ErrTooBig.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, bodyErr(err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooBig
	}
	return data, nil
}

func bodyErr(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return ErrBodyTooBig
	}
	return ErrBadForm
}

// Decode читает multipart-запрос и раскладывает JSON из части data по dst (без неизвестных полей).
// При ошибке сам отвечает по формату API (ошибки файлов попадают в поле field формы) и возвращает false.
func Decode(w http.ResponseWriter, r *http.Request, dst any, maxData int64, maxFiles int, field string) ([]Upload, bool) {
	form, err := ReadForm(w, r, maxData, maxFiles)
	if err != nil {
		WriteError(w, err, field)
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(form.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeBadRequest, "Не удалось прочитать запрос")
		return nil, false
	}
	return form.Uploads, true
}

// Messages — тексты ошибок для людей (по-русски).
const (
	msgEmpty   = "Файл пустой"
	msgTooBig  = "Файл больше 10 МБ"
	msgNotPDF  = "Файл не похож на PDF. Сохраните документ в формате PDF и загрузите его снова"
	msgTooMany = "Слишком много файлов"
)

// FieldMessage превращает ошибку файла в текст для человека; ok == false, если это не ошибка файлов.
func FieldMessage(err error) (string, bool) {
	var fe *FileError
	name := ""
	if errors.As(err, &fe) {
		name = "«" + fe.Name + "»: "
	}
	switch {
	case errors.Is(err, ErrEmpty):
		return name + msgEmpty, true
	case errors.Is(err, ErrTooBig):
		if name == "" {
			return "Все файлы вместе больше 25 МБ. Уберите лишние или сожмите их", true
		}
		return name + msgTooBig, true
	case errors.Is(err, ErrNotPDF):
		return name + msgNotPDF, true
	case errors.Is(err, ErrTooMany):
		return msgTooMany, true
	}
	return "", false
}

// WriteError отвечает на ошибку чтения запроса: слишком большой запрос — 413, ошибка файла — 422 с полем field, остальное — 400.
func WriteError(w http.ResponseWriter, err error, field string) {
	if errors.Is(err, ErrBodyTooBig) {
		apierr.WriteError(w, http.StatusRequestEntityTooLarge, CodeBodyTooBig, "Запрос слишком большой. Файлы должны быть не больше 10 МБ каждый и 25 МБ вместе")
		return
	}
	if msg, ok := FieldMessage(err); ok {
		apierr.WriteFieldErrors(w, "Проверьте файлы", map[string]string{field: msg})
		return
	}
	apierr.WriteError(w, http.StatusBadRequest, apierr.CodeBadRequest, "Не удалось прочитать запрос")
}
