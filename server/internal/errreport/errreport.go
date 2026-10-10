// Package errreport — отправка ошибок сервера в GlitchTip (срез 26, D-172): записи журнала уровня Error уходят в
// отдельный сервис, где они сгруппированы, посчитаны и видны разработчику с местом в коде. Сервис совместим с
// протоколом Sentry, поэтому здесь свой маленький клиент вместо большого SDK: он отправляет ровно то, что мы
// разрешили, и ничего «заодно».
//
// Критичная зона (docs/TESTING.md, персональные данные 152-ФЗ): отчёт об ошибке покидает сервер и хранится в чужой
// системе, поэтому в него попадает меньше, чем в журнал:
//   - запись уже прошла через logmask (почты, адреса людей, токены скрыты);
//   - из атрибутов берутся только названные в allowedKeys (технические: номер запроса, шаблон адреса, имя задачи,
//     код ошибки). Остальные ключи не отправляются, отправляются только их имена: так новое поле в журнале с чужим
//     именем или текстом письма не утечёт молча;
//   - ошибка PostgreSQL сводится к коду, таблице и ограничению: текст базы содержит значения («Key (email)=(...)»);
//   - пути к файлам в стеке обрезаются до трёх последних элементов (в абсолютном пути имя учётной записи
//     разработчика); ни аргументов функций, ни заголовков, ни тел запросов, ни адреса посетителя в отчёте нет.
//
// Отчёты никогда не мешают работе: очередь ограничена и неблокирующая, число отчётов в минуту ограничено, повторы
// одной ошибки сворачиваются, сбой отправки только считается.
package errreport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
)

// Настройки по умолчанию.
const (
	defaultQueue     = 100
	defaultPerMinute = 60
	defaultBurst     = 20
	defaultRepeat    = 10 * time.Second
	defaultTimeout   = 5 * time.Second
	maxFrames        = 50
	maxText          = 1000
	maxExtra         = 500
	defaultKinds     = 1000
)

// allowedKeys — атрибуты записи, которые отправляются целиком (после маски). Список закрытый: чужое имя, тема письма
// или текст отправляется только как имя ключа в omitted_keys.
var allowedKeys = map[string]bool{
	"err": true, "error": true, "panic": true, "stack": true, "request_id": true, "path": true, "route": true,
	"method": true, "status": true, "job": true, "failures": true, "failed_before": true, "query": true, "ms": true,
	"rows": true, "id": true, "file": true, "attempt": true, "attempts": true, "reason": true, "signature": true,
	"check": true, "skew": true, "allowed": true, "count": true, "service": true, "retry_after": true, "next": true,
	"waiting": true, "retrying": true, "undelivered": true, "oldest_waiting": true,
}

// Config — настройки отправителя.
type Config struct {
	// DSN вида https://ключ@хост/номер-проекта (GlitchTip → проект → Client Keys).
	DSN string
	// Release (версия сборки), Environment (development или production), ServerName (имя процесса: serve, worker).
	Release, Environment, ServerName string
	// Client — HTTP-клиент; по умолчанию со сроком 5 секунд.
	Client *http.Client
	// Queue — сколько отчётов ждёт отправки; сверх этого отчёты отбрасываются, а не задерживают работу.
	Queue int
	// PerMinute и Burst — общий предел отправки; Repeat — как часто может уходить отчёт об одной и той же ошибке.
	PerMinute, Burst int
	Repeat           time.Duration
	// Kinds — сколько разных ошибок помнится для сворачивания повторов; сверх этого память очищается.
	Kinds int
	// Now — часы; в тестах подменяются.
	Now func() time.Time
}

// Stats — счётчики отправителя (для метрик).
type Stats struct {
	Sent       int64 // принято сервисом
	Failed     int64 // не удалось отправить
	Dropped    int64 // отброшено из-за переполнения очереди или предела отправки
	Suppressed int64 // свёрнуто как повтор той же ошибки
}

// Reporter отправляет отчёты. Нулевой указатель — «отправки нет»: все методы безопасны.
type Reporter struct {
	cfg      Config
	endpoint string
	auth     string
	queue    chan []byte
	wg       sync.WaitGroup
	logMu    sync.Mutex
	logger   *slog.Logger
	// closeMu не даёт закрыть очередь в тот момент, когда в неё пишут.
	closeMu sync.RWMutex
	closed  bool
	// ctx отменяется, когда Close устал ждать: отправка в этот момент прерывается, остаток очереди отбрасывается.
	ctx    context.Context //nolint:containedctx // жизнь отправителя: один контекст на фоновую горутину отправки
	cancel context.CancelFunc

	mu       sync.Mutex
	tokens   float64
	refilled time.Time
	last     map[string]*seen

	sent, failed, dropped, suppressed atomic.Int64
	lastFailureLog                    atomic.Int64
}

type seen struct {
	at         time.Time
	suppressed int
}

// New разбирает DSN и запускает отправителя. Пустой DSN — ошибка: «ничего не отправлять» выражается отсутствием
// Reporter (nil), а не пустым адресом.
func New(cfg Config) (*Reporter, error) {
	endpoint, key, err := parseDSN(cfg.DSN)
	if err != nil {
		return nil, err
	}
	if cfg.Queue <= 0 {
		cfg.Queue = defaultQueue
	}
	if cfg.PerMinute <= 0 {
		cfg.PerMinute = defaultPerMinute
	}
	if cfg.Burst <= 0 {
		cfg.Burst = defaultBurst
	}
	if cfg.Repeat <= 0 {
		cfg.Repeat = defaultRepeat
	}
	if cfg.Kinds <= 0 {
		cfg.Kinds = defaultKinds
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: defaultTimeout}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Reporter{
		ctx: ctx, cancel: cancel,
		cfg: cfg, endpoint: endpoint,
		auth:     fmt.Sprintf("Sentry sentry_version=7, sentry_client=scibox/%s, sentry_key=%s", cleanToken(cfg.Release), key),
		queue:    make(chan []byte, cfg.Queue),
		tokens:   float64(cfg.Burst),
		refilled: cfg.Now(),
		last:     map[string]*seen{},
	}
	r.wg.Go(r.run)
	return r, nil
}

var tokenRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func cleanToken(s string) string {
	if s == "" {
		return "dev"
	}
	return tokenRe.ReplaceAllString(s, "_")
}

// ValidateDSN проверяет адрес приёма ошибок при чтении настроек: опечатка останавливает запуск, а не молча отключает
// сбор ошибок.
func ValidateDSN(dsn string) error {
	_, _, err := parseDSN(dsn)
	return err
}

// parseDSN возвращает адрес приёма событий и публичный ключ. Секретная часть DSN (устаревшая) не поддерживается.
func parseDSN(dsn string) (endpoint, key string, err error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", "", errors.New("errreport: DSN must look like https://key@host/project")
	}
	key = u.User.Username()
	if _, hasPassword := u.User.Password(); key == "" || hasPassword {
		return "", "", errors.New("errreport: DSN must carry a public key and no secret: https://key@host/project")
	}
	trimmed := strings.Trim(u.Path, "/")
	prefix, project, _ := cutLast(trimmed, "/")
	if project == "" || strings.Trim(project, "0123456789") != "" {
		return "", "", errors.New("errreport: DSN project must be a number: https://key@host/project")
	}
	base := u.Scheme + "://" + u.Host
	if prefix != "" {
		base += "/" + prefix
	}
	return base + "/api/" + project + "/store/", key, nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+len(sep):], true
}

// SetLogger задаёт журнал, в который отправитель пишет о своих сбоях. Это должен быть журнал без отправки ошибок:
// иначе сбой отправки породил бы ещё одну ошибку и так по кругу.
func (r *Reporter) SetLogger(l *slog.Logger) {
	if r == nil {
		return
	}
	r.logMu.Lock()
	r.logger = l
	r.logMu.Unlock()
}

// Stats возвращает счётчики.
func (r *Reporter) Stats() Stats {
	if r == nil {
		return Stats{}
	}
	return Stats{Sent: r.sent.Load(), Failed: r.failed.Load(), Dropped: r.dropped.Load(), Suppressed: r.suppressed.Load()}
}

// CloseWait — сколько Close ждёт отправки очереди при остановке процесса: недоступный GlitchTip не должен задерживать
// выход (сто отчётов по пять секунд — это восемь минут).
const CloseWait = 5 * time.Second

// Close дожидается отправки того, что уже в очереди, но не дольше CloseWait, и останавливает отправителя.
func (r *Reporter) Close() {
	if r == nil {
		return
	}
	r.closeMu.Lock()
	if r.closed {
		r.closeMu.Unlock()
		return
	}
	r.closed = true
	close(r.queue)
	r.closeMu.Unlock()
	finished := make(chan struct{})
	go func() { r.wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(CloseWait):
		r.cancel() // прерывает текущую отправку; остаток очереди пройдёт мгновенными сбоями
		<-finished
	}
	r.cancel()
}

func (r *Reporter) run() {
	for body := range r.queue {
		r.post(r.ctx, body)
	}
}

// Event — отчёт об одной ошибке до сборки в JSON.
type Event struct {
	Time    time.Time
	Message string
	// Err — текст ошибки, если запись несла ошибку.
	Err string
	// Where — функция и место записи в журнал (из PC записи).
	Func, File string
	Line       int
	// Stack — стек в виде текста runtime/debug.Stack (при панике); если пуст, берётся место записи.
	Stack string
	// Extra — атрибуты записи, прошедшие список allowedKeys.
	Extra map[string]string
	// Omitted — имена атрибутов, которые не отправлены.
	Omitted []string
	// RequestID и Route — привязка к запросу.
	RequestID, Route string
}

// Capture ставит отчёт в очередь, если не исчерпан предел и это не повтор недавней ошибки. Никогда не блокирует.
func (r *Reporter) Capture(ev Event) {
	if r == nil {
		return
	}
	r.closeMu.RLock()
	defer r.closeMu.RUnlock()
	if r.closed {
		return
	}
	fp := fingerprint(ev)
	suppressed, ok := r.admit(fp)
	if !ok {
		return
	}
	select {
	case r.queue <- r.build(ev, fp, suppressed):
	default:
		r.dropped.Add(1)
	}
}

// admit решает, отправлять ли отчёт: ограничение числа в минуту (ведро) и сворачивание повторов одной ошибки.
// Возвращает, сколько повторов этой ошибки было свёрнуто с прошлой отправки.
func (r *Reporter) admit(fp string) (suppressed int, ok bool) {
	now := r.cfg.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.last[fp]; s != nil && now.Sub(s.at) < r.cfg.Repeat {
		s.suppressed++
		r.suppressed.Add(1)
		return 0, false
	}
	if elapsed := now.Sub(r.refilled); elapsed > 0 {
		r.tokens = min(float64(r.cfg.Burst), r.tokens+elapsed.Minutes()*float64(r.cfg.PerMinute))
		r.refilled = now
	}
	if r.tokens < 1 {
		r.dropped.Add(1)
		return 0, false
	}
	r.tokens--
	s := r.last[fp]
	if s == nil {
		if len(r.last) >= r.cfg.Kinds {
			r.forgetOld(now)
		}
		s = &seen{}
		r.last[fp] = s
	}
	suppressed, s.at, s.suppressed = s.suppressed, now, 0
	return suppressed, true
}

// forgetOld освобождает память: забывает ошибки, не повторявшиеся дольше периода сворачивания, а если таких нет — всё.
func (r *Reporter) forgetOld(now time.Time) {
	for k, s := range r.last {
		if now.Sub(s.at) >= r.cfg.Repeat {
			delete(r.last, k)
		}
	}
	if len(r.last) >= r.cfg.Kinds {
		clear(r.last)
	}
}

// fingerprint — ключ группировки: то же сообщение из того же места в коде — одна ошибка, как бы ни менялись значения.
func fingerprint(ev Event) string {
	return clip(ev.Message, 200) + "\x00" + ev.Func
}

type frame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno,omitempty"`
	InApp    bool   `json:"in_app"`
}

type exception struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Stacktrace *struct {
		Frames []frame `json:"frames"`
	} `json:"stacktrace,omitempty"`
}

type payload struct {
	EventID     string            `json:"event_id"`
	Timestamp   string            `json:"timestamp"`
	Platform    string            `json:"platform"`
	Level       string            `json:"level"`
	Logger      string            `json:"logger"`
	Message     string            `json:"message"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	ServerName  string            `json:"server_name,omitempty"`
	Fingerprint []string          `json:"fingerprint"`
	Tags        map[string]string `json:"tags,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
	Exception   struct {
		Values []exception `json:"values"`
	} `json:"exception"`
}

// build собирает JSON отчёта. Структура состоит из строк, чисел и списков строк: Marshal не может вернуть ошибку.
func (r *Reporter) build(ev Event, fp string, suppressed int) []byte {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	p := payload{
		EventID:     hex.EncodeToString(id),
		Timestamp:   ev.Time.UTC().Format("2006-01-02T15:04:05"),
		Platform:    "go",
		Level:       "error",
		Logger:      "scibox",
		Message:     clip(ev.Message, maxText),
		Release:     r.cfg.Release,
		Environment: r.cfg.Environment,
		ServerName:  r.cfg.ServerName,
		Fingerprint: []string{fp},
		Tags:        map[string]string{},
		Extra:       map[string]any{},
	}
	if ev.Time.IsZero() {
		p.Timestamp = r.cfg.Now().UTC().Format("2006-01-02T15:04:05")
	}
	if ev.RequestID != "" {
		p.Tags["request_id"] = clip(ev.RequestID, 64)
	}
	if ev.Route != "" {
		p.Tags["route"] = clip(ev.Route, 120)
	}
	for k, v := range ev.Extra {
		if k != "stack" && k != "request_id" && k != "route" {
			p.Extra[k] = clip(v, maxExtra)
		}
	}
	if len(ev.Omitted) > 0 {
		omitted := slices.Clone(ev.Omitted)
		slices.Sort(omitted)
		p.Extra["omitted_keys"] = omitted
	}
	if suppressed > 0 {
		p.Extra["repeats_folded"] = suppressed
	}

	ex := exception{Type: "error", Value: clip(cmpOr(ev.Err, ev.Message), maxText)}
	var frames []frame
	if ev.Stack != "" {
		frames = parseStack(ev.Stack)
	}
	if len(frames) == 0 && ev.Func != "" {
		frames = []frame{{Filename: shortPath(ev.File), Function: ev.Func, Lineno: ev.Line, InApp: strings.HasPrefix(ev.Func, "scibox/")}}
	}
	if len(frames) > 0 {
		ex.Stacktrace = &struct {
			Frames []frame `json:"frames"`
		}{Frames: frames}
	}
	p.Exception.Values = []exception{ex}
	body, _ := json.Marshal(p)
	return body
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func clip(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// shortPath оставляет три последних элемента пути: `internal/httpapi/router.go`, `src/net/http/server.go`.
func shortPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	parts := strings.Split(p, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}

// parseStack разбирает текст runtime/debug.Stack. Кадры в нём идут от самого глубокого к внешнему, в протоколе
// наоборот. Служебные кадры самого сбора пропускаются.
func parseStack(text string) []frame {
	lines := strings.Split(text, "\n")
	var out []frame
	for i := 0; i+1 < len(lines); i++ {
		fn := strings.TrimSpace(lines[i])
		loc := lines[i+1]
		if fn == "" || strings.HasPrefix(fn, "goroutine ") || !strings.HasPrefix(loc, "\t") || !strings.HasSuffix(fn, ")") {
			continue
		}
		i++
		name := fn
		if open := strings.LastIndex(fn, "("); open > 0 {
			name = fn[:open]
		}
		if strings.HasPrefix(name, "runtime/debug.") || strings.HasPrefix(name, "scibox/server/internal/errreport.") {
			continue
		}
		file, rest, _ := strings.Cut(strings.TrimSpace(loc), " ")
		_ = rest
		line := 0
		if c := strings.LastIndex(file, ":"); c >= 0 {
			line, _ = strconv.Atoi(file[c+1:])
			file = file[:c]
		}
		out = append(out, frame{Filename: shortPath(file), Function: name, Lineno: line, InApp: strings.HasPrefix(name, "scibox/")})
		if len(out) >= maxFrames {
			break
		}
	}
	// Кадры самого перехвата (отложенная функция, журнал) идут до `panic`: отбрасываются вместе с ним и с кадрами
	// среды выполнения, чтобы на вершине стоял код, который упал.
	if i := slices.IndexFunc(out, func(f frame) bool { return f.Function == "panic" }); i >= 0 {
		out = out[i+1:]
		for len(out) > 0 && strings.HasPrefix(out[0].Function, "runtime.") {
			out = out[1:]
		}
	}
	slices.Reverse(out)
	return out
}

// post отправляет один отчёт; сбой только считается и редко пишется в журнал.
func (r *Reporter) post(ctx context.Context, body []byte) {
	if err := r.send(ctx, body); err != nil {
		r.failed.Add(1)
		r.noteFailure(err)
		return
	}
	r.sent.Add(1)
}

func (r *Reporter) send(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sentry-Auth", r.auth)
	resp, err := r.cfg.Client.Do(req)
	if err != nil {
		// Client.Do всегда возвращает *url.Error; в нём адрес с путём проекта, а нужен только вид сбоя.
		return fmt.Errorf("send error report: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close() //nolint:errcheck // тело ответа прочитано выше; закрытие ничего не пишет
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("error report refused: status %d", resp.StatusCode)
	}
	return nil
}

// noteFailure пишет в журнал о сбое отправки не чаще раза в минуту.
func (r *Reporter) noteFailure(err error) {
	now := r.cfg.Now().Unix()
	last := r.lastFailureLog.Load()
	if now-last < 60 || !r.lastFailureLog.CompareAndSwap(last, now) {
		return
	}
	r.logMu.Lock()
	l := r.logger
	r.logMu.Unlock()
	if l != nil {
		l.Warn("error reports are not delivered", "err", err, "failed_total", r.failed.Load())
	}
}

// SendTest отправляет пробный отчёт сразу и возвращает результат: так проверяют, что адрес и ключ верны
// (`scibox error-test`). Пределы и сворачивание повторов не действуют.
func (r *Reporter) SendTest(ctx context.Context) error {
	if r == nil {
		return errors.New("errreport: error reports are not configured (SCIBOX_ERROR_DSN is empty)")
	}
	ev := Event{
		Time: r.cfg.Now(), Message: "test event from `scibox error-test`", Err: "this is a test: nothing is broken",
		Func: "scibox/server/internal/errreport.SendTest", File: "internal/errreport/errreport.go",
		Extra: map[string]string{"service": r.cfg.ServerName},
	}
	return r.send(ctx, r.build(ev, "scibox-error-test", 0))
}

// Handler — обработчик slog: передаёт запись дальше и отправляет отчёт о записях уровня Error. Ставится под logmask:
// запись к нему приходит уже очищенной.
type Handler struct {
	next slog.Handler
	r    *Reporter
}

// NewHandler оборачивает next. С nil вместо отправителя записи просто идут дальше.
func NewHandler(next slog.Handler, r *Reporter) *Handler { return &Handler{next: next, r: r} }

// Enabled передаёт вопрос дальше.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle отправляет отчёт и передаёт запись дальше.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelError && h.r != nil {
		h.r.Capture(fromRecord(ctx, rec))
	}
	return h.next.Handle(ctx, rec)
}

// WithAttrs передаёт атрибуты дальше. Атрибуты логгера (service, version) в отчёт не копируются: они в метках отправителя.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), r: h.r}
}

// WithGroup передаёт группу дальше.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), r: h.r}
}

// fromRecord собирает отчёт из записи журнала.
func fromRecord(ctx context.Context, rec slog.Record) Event {
	ev := Event{Time: rec.Time, Message: rec.Message, Extra: map[string]string{}}
	if rec.PC != 0 {
		f, _ := runtime.CallersFrames([]uintptr{rec.PC}).Next()
		ev.Func, ev.File, ev.Line = f.Function, f.File, f.Line
	}
	rec.Attrs(func(a slog.Attr) bool {
		key := a.Key
		if !allowedKeys[key] {
			ev.Omitted = append(ev.Omitted, safeKey(key))
			return true
		}
		val := a.Value.Resolve().String()
		switch key {
		case "request_id":
			ev.RequestID = val
		case "route":
			ev.Route = val
		case "stack":
			ev.Stack = val
		case "err", "error":
			ev.Err = val
			ev.Extra[key] = val
		default:
			ev.Extra[key] = val
		}
		return true
	})
	if ev.Route == "" {
		if rc := chi.RouteContext(ctx); rc != nil {
			ev.Route = rc.RoutePattern()
		}
	}
	// Ошибка базы в тексте несёт значения («Key (email)=(…) already exists»): оставляем код и место, текст базы нет.
	if ev.Err != "" {
		ev.Err = scrubPostgres(ev.Err)
		if _, ok := ev.Extra["err"]; ok {
			ev.Extra["err"] = ev.Err
		}
		if _, ok := ev.Extra["error"]; ok {
			ev.Extra["error"] = ev.Err
		}
	}
	return ev
}

var pgErrRe = regexp.MustCompile(`(?s)ERROR: .*?\(SQLSTATE ([0-9A-Z]{5})\)`)

// scrubPostgres заменяет текст ошибки PostgreSQL, как её печатает pgx (`ERROR: … (SQLSTATE 23505)`), на код.
func scrubPostgres(s string) string {
	return pgErrRe.ReplaceAllString(s, "database error (SQLSTATE $1)")
}

func safeKey(k string) string {
	k = clip(k, 40)
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '_'
		}
		return r
	}, k)
}
