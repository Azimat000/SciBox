package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
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

// Form — разобранный запрос: JSON из части data и принятые файлы из частей file в порядке следования.
type Form struct {
	Data  []byte
	Files []Stored
}

// ReadForm читает multipart-запрос: часть `data` (JSON не больше maxData байт) и до maxFiles частей `file`.
// Часть `data` должна идти первой: после неё вызывается afterData (если задана), и только потом принимаются файлы.
// Так запрос, который всё равно будет отвергнут (неверные поля, мёртвая ссылка), не заставляет хранилище и антивирус
// работать впустую.
// Файлы принимает Vault.Put прямо из запроса, по одному и потоком: в памяти не бывает больше одной порции файла,
// а не заведомо проверенное (не PDF, больше предела, вирус) в хранилище не остаётся. Все файлы вместе не больше
// MaxTotalSize: лишнее обрывается, не дожидаясь конца запроса. При любой ошибке уже принятые файлы удаляются;
// при успехе они переходят во владение вызывающего.
func ReadForm(ctx context.Context, w http.ResponseWriter, r *http.Request, v *Vault, maxData int64, maxFiles int, afterData func(data []byte) error) (form Form, err error) {
	defer func() {
		if err != nil {
			v.Discard(ctx, form.Files...)
			form = Form{}
		}
	}()
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		return form, ErrBadForm
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxData+int64(maxFiles)*MaxFileSize+bodyOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return form, ErrBadForm
	}
	haveData := false
	var total int64
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return form, bodyErr(err)
		}
		switch part.FormName() {
		case partData:
			if haveData {
				return form, ErrBadForm
			}
			haveData = true
			if form.Data, err = readLimited(part, maxData); err != nil {
				return form, err
			}
			if afterData != nil {
				if err := afterData(form.Data); err != nil {
					return form, err
				}
			}
		case partFile:
			if !haveData {
				return form, ErrBadForm
			}
			if len(form.Files) >= maxFiles {
				return form, ErrTooMany
			}
			st, err := v.Put(ctx, part.FileName(), part)
			if err != nil {
				return form, partErr(err)
			}
			form.Files = append(form.Files, st)
			if total += st.Size; total > MaxTotalSize {
				return form, ErrTooBig
			}
		default:
			return form, ErrBadForm
		}
	}
	if !haveData {
		return form, ErrBadForm
	}
	return form, nil
}

// partErr приводит ошибку приёма файла к ошибкам этого раздела: сбой чтения запроса становится ErrBodyTooBig или
// ErrBadForm, остальное (ошибка файла, сбой хранилища) остаётся как есть.
func partErr(err error) error {
	var fe *FileError
	if errors.As(err, &fe) || errors.Is(err, ErrStorage) {
		return err
	}
	return bodyErr(err)
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

// ErrBadJSON — часть `data` не разобралась как JSON с известными полями.
var ErrBadJSON = errors.New("files: bad json in the data part")

// Decode читает multipart-запрос и раскладывает JSON из части data по dst (без неизвестных полей). Сразу после этого,
// до приёма файлов, вызывается check (если задана): она проверяет поля и права и может отвергнуть запрос. При ошибке
// принятые файлы уже удалены, а сама ошибка возвращается как есть: ошибки загрузки (IsUploadError) показывает
// WriteError, остальные — вызывающий. При успехе принятые файлы переходят во владение вызывающего.
func Decode(ctx context.Context, w http.ResponseWriter, r *http.Request, v *Vault, dst any, maxData int64, maxFiles int, check func() error) ([]Stored, error) {
	form, err := ReadForm(ctx, w, r, v, maxData, maxFiles, func(data []byte) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			return ErrBadJSON
		}
		if check != nil {
			return check()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return form.Files, nil
}

// IsUploadError — ошибка загрузки (форма, файл, размер, хранилище), а не решение вызывающего кода.
func IsUploadError(err error) bool {
	var fe *FileError
	return errors.As(err, &fe) || errors.Is(err, ErrBadForm) || errors.Is(err, ErrBadJSON) || errors.Is(err, ErrTooMany) ||
		errors.Is(err, ErrTooBig) || errors.Is(err, ErrBodyTooBig) || errors.Is(err, ErrStorage)
}

// Messages — тексты ошибок для людей (по-русски).
const (
	msgEmpty   = "Файл пустой"
	msgTooBig  = "Файл больше 10 МБ"
	msgNotPDF  = "Файл не похож на PDF. Сохраните документ в формате PDF и загрузите его снова"
	msgTooMany = "Слишком много файлов"
	// msgInfected не называет вирус: человеку это ничего не скажет, а название уходит в журнал сервера.
	msgInfected = "Антивирус нашёл в файле вирус, поэтому он не принят. Проверьте компьютер и загрузите другой файл"
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
	case errors.Is(err, ErrInfected):
		return name + msgInfected, true
	}
	return "", false
}

// WriteError отвечает на ошибку чтения запроса: слишком большой запрос — 413, ошибка файла — 422 с полем field, остальное — 400.
func WriteError(w http.ResponseWriter, err error, field string) {
	if errors.Is(err, ErrStorage) {
		WriteStorageUnavailable(w)
		return
	}
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

// CodeStorageUnavailable — хранилище файлов не отвечает.
const CodeStorageUnavailable = "storage_unavailable"

// WriteStorageUnavailable отвечает 503: файл не принят или не выдан из-за хранилища, человеку стоит повторить позже.
func WriteStorageUnavailable(w http.ResponseWriter) {
	apierr.WriteError(w, http.StatusServiceUnavailable, CodeStorageUnavailable, "Хранилище файлов временно недоступно. Попробуйте ещё раз через несколько минут")
}
