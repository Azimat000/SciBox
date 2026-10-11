// Package files — файлы откликов: что принимаем, как проверяем и кому отдаём. Это критичная зона (docs/TESTING.md):
// ошибка здесь отдаёт рекомендательное письмо самому соискателю или принимает не PDF, а исполняемый файл.
//
// Принимаем только PDF (D-080). Сами файлы лежат в S3-совместимом хранилище, в базе только строка с ключом и
// результатом проверки на вирусы (D-157). Пакет не знает про базу: он принимает загрузку потоком (Vault), читает
// multipart-запрос, решает «вид файла → кто видит» и отдаёт файл безопасно.
package files

import (
	"errors"
	"io"
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

// CheckSet проверяет набор принятых файлов: число и общий размер. Каждый файл уже проверен при записи (Vault.Put).
func CheckSet(set []Stored) error {
	if len(set) > MaxAttachments {
		return ErrTooMany
	}
	var total int64
	for _, f := range set {
		total += f.Size
	}
	if total > MaxTotalSize {
		return ErrTooBig
	}
	return nil
}

// spoofing — знаки, которые меняют вид имени, не будучи видимыми: переключатели направления письма (из «report\u202etxt.exe»
// получается «reportexe.txt»), разделители строк и абзацев, невидимый пробел и метка порядка байт. Знаки U+200C и U+200D
// остаются: без них неверно пишутся имена на персидском, хинди и другие.
func spoofing(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	switch r {
	case 0x200B, 0x200E, 0x200F, 0x061C, 0x2028, 0x2029, 0xFEFF:
		return true
	}
	return false
}

// CleanName делает из имени файла безопасное и читаемое: только последняя часть пути, без служебных знаков,
// не длиннее предела, с расширением .pdf.
func CleanName(raw string) string {
	name := strings.ReplaceAll(raw, `\`, "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), strings.ContainsRune(`/\:*?"<>|`, r), spoofing(r):
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
	base = strings.Trim(base, ". \t") // точки и пробелы по краям вперемешку: «. .x» не должно оставлять скрытое имя
	if base == "" {
		base = "файл"
	}
	if r := []rune(base); len(r) > maxNameRunes-4 {
		base = strings.Trim(string(r[:maxNameRunes-4]), ". \t")
		if base == "" {
			base = "файл"
		}
	}
	return base + ".pdf"
}

// Serve отдаёт PDF как скачиваемый файл потоком из хранилища. Браузеру запрещено угадывать тип и исполнять содержимое.
// Длина известна заранее: если поток оборвётся посреди файла, соединение закроется и браузер увидит неполную загрузку,
// а не «успешно скачанный» обрезанный файл. Ошибка копирования возвращается для журнала.
func Serve(w http.ResponseWriter, name string, size int64, body io.Reader) error {
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `attachment; filename="file.pdf"; filename*=UTF-8''`+url.PathEscape(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	n, err := io.Copy(w, body)
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	return err
}
