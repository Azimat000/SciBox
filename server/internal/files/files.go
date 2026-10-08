// Package files — файлы откликов: что принимаем, как проверяем и кому отдаём. Это критичная зона (docs/TESTING.md):
// ошибка здесь отдаёт рекомендательное письмо самому соискателю или принимает не PDF, а исполняемый файл.
//
// Принимаем только PDF (D-080). Сами файлы лежат в базе (таблица application_files); пакет не знает про базу:
// он проверяет загрузки, читает multipart-запрос, решает «вид файла → кто видит» и отдаёт файл безопасно.
package files

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Пределы. Размеры в байтах.
const (
	// MaxFileSize — самый большой файл.
	MaxFileSize = 10 << 20
	// MaxAttachments — сколько файлов можно приложить к отклику (резюме из профиля сюда не входит).
	MaxAttachments = 5
	// MaxTotalSize — все приложенные файлы вместе.
	MaxTotalSize = 25 << 20
	// maxNameRunes — самое длинное имя файла в знаках.
	maxNameRunes = 120
	// headerWindow — где в начале файла должна стоять метка %PDF- (стандарт разрешает немного лишнего перед ней).
	headerWindow = 1024
)

// Kind — вид файла отклика. Строки лежат в базе, не менять.
type Kind string

const (
	// KindCV — резюме, собранное из профиля при отклике.
	KindCV Kind = "cv"
	// KindAttachment — файл, который приложил соискатель.
	KindAttachment Kind = "attachment"
	// KindReferenceLetter — рекомендательное письмо в PDF, загруженное рекомендателем.
	KindReferenceLetter Kind = "reference_letter"
)

// Kinds — все виды файлов.
var Kinds = []Kind{KindCV, KindAttachment, KindReferenceLetter}

// Parse превращает строку в вид файла; ok == false, если такого вида нет.
func Parse(s string) (Kind, bool) {
	switch k := Kind(s); k {
	case KindCV, KindAttachment, KindReferenceLetter:
		return k, true
	}
	return "", false
}

// Reader — кто просит файл. Нулевое значение — никто (не соискатель и не сотрудник организации).
type Reader int

const (
	// Applicant — человек, который откликнулся.
	Applicant Reader = iota + 1
	// Staff — сотрудник организации, который вправе видеть отклики на эту вакансию.
	Staff
)

// VisibleTo — может ли этот читатель получить файл такого вида. Рекомендательные письма видит только организация
// (D-014): соискатель не должен знать, что о нём написали. Неизвестный вид закрыт для всех.
func (k Kind) VisibleTo(r Reader) bool {
	switch k {
	case KindCV, KindAttachment:
		return r == Applicant || r == Staff
	case KindReferenceLetter:
		return r == Staff
	}
	return false
}

// Upload — загруженный файл.
type Upload struct {
	Name string
	Data []byte
}

// Ошибки проверки. В тексте — что сказать человеку; имя файла сообщает вызывающий.
var (
	ErrEmpty   = errors.New("files: empty file")
	ErrTooBig  = errors.New("files: file is too big")
	ErrNotPDF  = errors.New("files: not a pdf")
	ErrTooMany = errors.New("files: too many files")
	ErrBadForm = errors.New("files: bad multipart form")
	// ErrBodyTooBig — запрос целиком больше допустимого.
	ErrBodyTooBig = errors.New("files: request is too big")
)

// FileError — ошибка с конкретным файлом: по имени можно сказать человеку, какой именно не подошёл.
type FileError struct {
	Name string
	Err  error
}

func (e *FileError) Error() string { return e.Err.Error() + ": " + e.Name }
func (e *FileError) Unwrap() error { return e.Err }

var pdfMagic = []byte("%PDF-")

// Check проверяет один файл: не пустой, не больше предела и начинается с метки PDF. Расширению и типу из запроса не верим.
func Check(u Upload) error {
	switch {
	case len(u.Data) == 0:
		return ErrEmpty
	case len(u.Data) > MaxFileSize:
		return ErrTooBig
	}
	head := u.Data[:min(len(u.Data), headerWindow)]
	if !bytes.Contains(head, pdfMagic) {
		return ErrNotPDF
	}
	return nil
}

// CheckSet проверяет набор приложенных файлов: число, каждый файл и общий размер.
func CheckSet(uploads []Upload) error {
	if len(uploads) > MaxAttachments {
		return ErrTooMany
	}
	total := 0
	for _, u := range uploads {
		if err := Check(u); err != nil {
			return &FileError{Name: CleanName(u.Name), Err: err}
		}
		total += len(u.Data)
	}
	if total > MaxTotalSize {
		return ErrTooBig
	}
	return nil
}

// CleanName делает из имени файла безопасное и читаемое: только последняя часть пути, без служебных знаков,
// не длиннее предела, с расширением .pdf.
func CleanName(raw string) string {
	name := strings.ReplaceAll(raw, `\`, "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), strings.ContainsRune(`/\:*?"<>|`, r):
			return -1
		case r == utf8.RuneError:
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	base := name
	if strings.HasSuffix(strings.ToLower(name), ".pdf") {
		base = name[:len(name)-4]
	}
	base = strings.TrimSpace(strings.Trim(base, "."))
	if base == "" {
		base = "файл"
	}
	if r := []rune(base); len(r) > maxNameRunes-4 {
		base = strings.TrimSpace(string(r[:maxNameRunes-4]))
	}
	return base + ".pdf"
}

// Serve отдаёт PDF как скачиваемый файл. Браузеру запрещено угадывать тип и исполнять содержимое.
func Serve(w http.ResponseWriter, name string, data []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `attachment; filename="file.pdf"; filename*=UTF-8''`+url.PathEscape(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
