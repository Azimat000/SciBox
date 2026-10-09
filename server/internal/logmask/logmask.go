// Package logmask — маскирование персональных данных и секретов в журнале сервера (срез 22, T-18, D-152).
//
// Критичная зона (docs/TESTING.md): журнал сервера читают разработчики и системы сбора логов, а не только владелец
// данных, поэтому почты, адреса людей и токены в нём — утечка по 152-ФЗ. Маскирование стоит не в каждом месте записи
// (их десятки, и новое всегда можно забыть), а одним слоем под всем журналом: Handler оборачивает любой slog.Handler
// и чистит сообщение и все значения до того, как запись уйдёт дальше.
//
// Что скрывается:
//   - почты (`a.petrova@msu.ru` → `***@msu.ru`: домен нужен, чтобы понять отказ почтового сервера);
//   - адреса людей (IPv4 до сети /24, IPv6 до /48); служебные адреса (127.0.0.1, 10.0.0.0/8, 0.0.0.0, link-local)
//     остаются: это адрес самого сервера, а не человека;
//   - значения под ключами `password`, `token`, `cookie`, `authorization`, `secret`, `dsn`, `api_key` целиком;
//   - пары `password=…`, `token=…`, `Bearer …` внутри текста;
//   - пароль в адресе подключения `postgres://user:пароль@хост`;
//   - длинные случайные строки (токены сеансов и ссылок: 32 и больше знаков base64 или hex), кроме номеров uuid и
//     читаемых имён (`moskovskii-gosudarstvennyi-universitet`).
//
// Чего не умеет: имена людей и свободный текст распознать нельзя. Правило для кода: имена, тексты писем и контакты в
// журнал не пишутся (в записях только номера). Управляющие знаки в значениях заменяются пробелами (подделка строк журнала).
package logmask

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Hidden — чем заменяется скрытое целиком.
const Hidden = "[скрыто]"

var (
	emailRe    = regexp.MustCompile(`[\p{L}\p{N}._%+'-]+@([\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+)`)
	userinfoRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]*):[^/\s@]+@`)
	pairRe     = regexp.MustCompile(`(?i)(password|passwd|pwd|passphrase|secret|token|api[_-]?key|authorization|cookie|dsn)(["']?\s*[:=]\s*)(["']?)(?:bearer\s+|basic\s+)?[^\s,;&"'}]+`)
	bearerRe   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	longRe     = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)
	dottedRe   = regexp.MustCompile(`\d+(?:\.\d+){3,}(?:/\d{1,3})?`)
	colonRe    = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:\.\d{1,3}){0,3}(?:/\d{1,3})?`)
	uuidRe     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// sensitiveKeys — части имени атрибута, при которых значение скрывается целиком.
var sensitiveKeys = []string{"password", "passwd", "passphrase", "secret", "token", "cookie", "authorization", "credential", "dsn", "apikey", "api_key", "api-key"}

func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Mask чистит одну строку.
func Mask(s string) string {
	if s == "" {
		return s
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if strings.Contains(s, "://") {
		s = userinfoRe.ReplaceAllString(s, "${1}:"+Hidden+"@")
	}
	if strings.ContainsAny(s, "=:") {
		s = pairRe.ReplaceAllString(s, "${1}${2}${3}"+Hidden)
	}
	if len(s) >= 14 {
		s = bearerRe.ReplaceAllString(s, "${1} "+Hidden)
	}
	if strings.Contains(s, "@") {
		s = emailRe.ReplaceAllString(s, "***@${1}")
	}
	if len(s) >= 32 {
		s = longRe.ReplaceAllStringFunc(s, maskLong)
	}
	// Сначала IPv6 (в нём бывает вложенный IPv4), потом IPv4. Результат маскирования сам годится на вход: повторный
	// проход ничего не меняет.
	if strings.Count(s, ":") >= 2 {
		s = colonRe.ReplaceAllStringFunc(s, maskNet)
	}
	if strings.Contains(s, ".") {
		s = dottedRe.ReplaceAllStringFunc(s, maskNet)
	}
	return s
}

// maskLong скрывает случайную строку, но оставляет номера uuid и читаемые имена из строчных слов через - или _.
func maskLong(run string) string {
	if uuidRe.MatchString(run) || readableName(run) {
		return run
	}
	return Hidden
}

func readableName(s string) bool {
	if !strings.ContainsAny(s, "-_") {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// maskNet приводит адрес (с длиной сети или без) к сети; не адрес оставляет как есть.
func maskNet(s string) string {
	host, length, hasLength := strings.Cut(s, "/")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return s
	}
	bits := -1
	if hasLength {
		if n, err := strconv.Atoi(length); err == nil {
			bits = n
		}
	}
	return maskAddr(addr, bits, s)
}

// maskAddr оставляет адрес сервера как есть, а адрес человека сводит к сети; bits — уже известная длина сети (-1: нет).
func maskAddr(addr netip.Addr, bits int, orig string) string {
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsUnspecified() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
		return orig
	}
	limit := 24
	if addr.Is6() {
		limit = 48
	}
	if bits < 0 || bits > limit {
		bits = limit
	}
	return netip.PrefixFrom(addr, bits).Masked().String()
}

// Handler — slog.Handler, чистящий запись перед передачей дальше.
type Handler struct{ next slog.Handler }

// NewHandler оборачивает next.
func NewHandler(next slog.Handler) *Handler { return &Handler{next: next} }

// Enabled передаёт вопрос дальше.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle чистит сообщение и значения и передаёт запись дальше.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, Mask(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(maskAttr(a))
		return true
	})
	return h.next.Handle(ctx, clean)
}

// WithAttrs чистит атрибуты, которые будут у всех записей.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = maskAttr(a)
	}
	return &Handler{next: h.next.WithAttrs(masked)}
}

// WithGroup передаёт группу дальше: имя группы задаёт код, а не человек.
func (h *Handler) WithGroup(name string) slog.Handler { return &Handler{next: h.next.WithGroup(name)} }

func maskAttr(a slog.Attr) slog.Attr {
	if sensitiveKey(a.Key) {
		return slog.String(Mask(a.Key), Hidden)
	}
	return slog.Attr{Key: Mask(a.Key), Value: maskValue(a.Value)}
}

func maskValue(v slog.Value) slog.Value {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.StringValue(Mask(v.String()))
	case slog.KindGroup:
		group := v.Group()
		masked := make([]slog.Attr, len(group))
		for i, a := range group {
			masked[i] = maskAttr(a)
		}
		return slog.GroupValue(masked...)
	case slog.KindAny:
		// Ошибки, адреса, структуры и всё остальное печатается как текст и чистится: так значение не обходит маску,
		// пряча почту внутри типа.
		return slog.StringValue(Mask(fmt.Sprint(v.Any())))
	default:
		return v
	}
}
