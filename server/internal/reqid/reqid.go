// Package reqid — номер запроса (срез 26): один номер на запрос, он виден человеку в заголовке ответа `X-Request-Id`
// и стоит в каждой строке журнала, написанной во время этого запроса. По номеру из письма в поддержку находят все
// строки одного запроса, не гадая по времени и адресу.
//
// Номер всегда создаёт сервер. Присланный посетителем заголовок `X-Request-Id` не используется: в нём может быть
// что угодно (подделка строк журнала, чужие номера), а за доверенным прокси у прокси свой номер, который нам не нужен.
package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
)

// Header — заголовок ответа с номером запроса.
const Header = "X-Request-Id"

// Key — имя поля в журнале.
const Key = "request_id"

type ctxKey struct{}

// New создаёт номер: 16 случайных байт, 32 знака.
func New() string {
	var b [16]byte
	// crypto/rand.Read с Go 1.24 не возвращает ошибку: при сбое источника случайности процесс завершается.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// With кладёт номер в контекст.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From достаёт номер из контекста; пусто, если контекст не от запроса.
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Middleware даёт запросу номер: кладёт в контекст и в заголовок ответа. Ставится первым в роутере.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := New()
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r.WithContext(With(r.Context(), id)))
	})
}

// Handler дописывает номер запроса в каждую запись журнала, написанную с контекстом запроса
// (`logger.InfoContext(ctx, …)`); в записи без запроса или с уже указанным номером ничего не меняется.
type Handler struct{ next slog.Handler }

// NewHandler оборачивает next.
func NewHandler(next slog.Handler) *Handler { return &Handler{next: next} }

// Enabled передаёт вопрос дальше.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle дописывает номер и передаёт запись дальше.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := From(ctx); id != "" {
		has := false
		r.Attrs(func(a slog.Attr) bool {
			has = a.Key == Key
			return !has
		})
		if !has {
			r = r.Clone()
			r.AddAttrs(slog.String(Key, id))
		}
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs передаёт атрибуты дальше.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs)}
}

// WithGroup передаёт группу дальше.
func (h *Handler) WithGroup(name string) slog.Handler { return &Handler{next: h.next.WithGroup(name)} }
