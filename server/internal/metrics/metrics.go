// Package metrics — метрики Prometheus сервера и воркера (срез 26, T-25): запросы и задержки по адресам, паники, записи
// журнала по уровням, запросы к базе. Пакет ничего не знает о предметной области: он листовой, чтобы им могли
// пользоваться и роутер, и фоновые задачи, и пул базы, не создавая циклов. Сборщики состояния очередей и задач лежат
// в `internal/opsmetrics`.
//
// Главное правило пакета: у метки конечное число значений. Адрес в метке — шаблон маршрута (`/api/vacancies/{id}`),
// а не путь запроса; метод и код ответа приводятся к известным значениям. Посетитель не может раздуть число рядов
// в Prometheus, как бы он ни менял адрес (`TestRouteLabelNeverFollowsThePath`).
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Namespace — общий префикс имён метрик.
const Namespace = "scibox"

// Метки адреса, которым нет шаблона маршрута.
const (
	RouteSite    = "site"        // страница сайта (запрос вне /api)
	RouteUnknown = "api_unknown" // адрес под /api, которого нет в роутере
)

// latencyBuckets — границы корзин задержки в секундах: цели из docs/SCALING.md (чтение 0,2 с, запись 0,4 с, p99 1 с).
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.4, 0.75, 1, 2, 5, 10, 30}

// Metrics — набор метрик одного процесса.
type Metrics struct {
	Registry *prometheus.Registry

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
	panics   *prometheus.CounterVec
	logs     *prometheus.CounterVec

	jobRuns     *prometheus.CounterVec
	jobDuration *prometheus.HistogramVec

	queryDuration prometheus.Histogram
	slowQueries   *prometheus.CounterVec
	timeouts      *prometheus.CounterVec
	queryNames    *boundedSet

	// Живой поток чата (срез 39): открытые потоки, закрытия и отказы по причине, события по виду и этапу, задержка
	// доставки, состояние соединения LISTEN, добор пропущенного.
	chatStreams  prometheus.Gauge
	chatClosed   *prometheus.CounterVec
	chatRejected *prometheus.CounterVec
	chatEvents   *prometheus.CounterVec
	chatDelivery prometheus.Histogram
	chatLifetime prometheus.Histogram
	chatListener prometheus.Gauge
	chatCatchUp  prometheus.Counter
}

// New создаёт реестр с метриками Go и процесса и метриками сервера. service — имя процесса (serve, worker), version —
// версия сборки; они попадают в `scibox_build_info`.
func New(service, version string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		Registry: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "http", Name: "requests_total",
			Help: "Запросы по методу, шаблону адреса и коду ответа.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace, Subsystem: "http", Name: "request_duration_seconds",
			Help: "Время обработки запроса по методу и шаблону адреса.", Buckets: latencyBuckets,
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace, Subsystem: "http", Name: "requests_in_flight",
			Help: "Запросы, которые обрабатываются сейчас.",
		}),
		panics: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Name: "panics_total",
			Help: "Паники, перехваченные в обработчиках запросов (where=http) и фоновых задачах (where=job).",
		}, []string{"where"}),
		logs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Name: "log_events_total",
			Help: "Записи журнала по уровню.",
		}, []string{"level"}),
		jobRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "job", Name: "runs_total",
			Help: "Проходы фоновых задач этого процесса по имени и итогу: ok, failed, interrupted (остановка процесса).",
		}, []string{"task", "result"}),
		jobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace, Subsystem: "job", Name: "duration_seconds",
			Help: "Время прохода фоновой задачи этого процесса.", Buckets: []float64{0.01, 0.1, 0.5, 1, 5, 15, 60, 300, 900},
		}, []string{"task"}),
		queryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: Namespace, Subsystem: "db", Name: "query_duration_seconds",
			Help: "Время запросов к базе.", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2.5, 5, 10},
		}),
		slowQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "db", Name: "slow_queries_total",
			Help: "Запросы к базе медленнее порога SCIBOX_DB_SLOW_QUERY, по имени запроса.",
		}, []string{"query"}),
		timeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "db", Name: "query_timeouts_total",
			Help: "Запросы, остановленные базой по сроку (statement_timeout), по имени запроса.",
		}, []string{"query"}),
		queryNames: newBoundedSet(maxQueryNames),
		chatStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "streams_open",
			Help: "Открытые живые потоки чата этой копии.",
		}),
		chatClosed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "streams_closed_total",
			Help: "Закрытые потоки чата по причине: client, slow, rights, session, age, shutdown.",
		}, []string{"reason"}),
		chatRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "streams_rejected_total",
			Help: "Отказы в потоке чата: user_limit, copy_limit, shutdown.",
		}, []string{"reason"}),
		chatEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "events_total",
			Help: "События чата по виду и этапу: dispatched (копия обработала событие), delivered (получатель получил его).",
		}, []string{"kind", "stage"}),
		chatDelivery: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "delivery_seconds",
			Help: "От записи события чата в журнал до записи в поток получателя (цель среза 39: p95 не больше 2 секунд).", Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}),
		chatLifetime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "stream_lifetime_seconds",
			Help: "Сколько жил поток чата.", Buckets: []float64{1, 5, 15, 60, 120, 240, 300, 600},
		}),
		chatListener: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "listener_up",
			Help: "1, пока соединение LISTEN с базой поднято: без него копия узнаёт о событиях только страховочным опросом.",
		}),
		chatCatchUp: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: Namespace, Subsystem: "chat", Name: "catchup_events_total",
			Help: "События журнала, которые копия добрала, потому что уведомление не пришло (обрыв LISTEN, опрос).",
		}),
	}
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace, Name: "build_info",
		Help: "Версия сборки и процесс; значение всегда 1.",
	}, []string{"service", "version", "go_version"})
	build.WithLabelValues(label(service), label(version), runtime.Version()).Set(1)
	reg.MustRegister(m.requests, m.duration, m.inFlight, m.panics, m.logs, m.queryDuration, m.slowQueries, m.timeouts, m.jobRuns, m.jobDuration, build,
		m.chatStreams, m.chatClosed, m.chatRejected, m.chatEvents, m.chatDelivery, m.chatLifetime, m.chatListener, m.chatCatchUp)
	// Ряды, которые должны существовать с нуля: тревога «стало больше» не видит первого события, если ряда ещё нет.
	m.panics.WithLabelValues("http")
	m.panics.WithLabelValues("job")
	for _, l := range []string{"debug", "info", "warn", "error"} {
		m.logs.WithLabelValues(l)
	}
	for _, r := range []string{"client", "slow", "rights", "session", "age", "shutdown"} {
		m.chatClosed.WithLabelValues(r)
	}
	for _, r := range []string{"user_limit", "copy_limit", "shutdown"} {
		m.chatRejected.WithLabelValues(r)
	}
	return m
}

// maxQueryNames — сколько разных имён запросов получают свою метку; остальные собираются в «other».
const maxQueryNames = 300

// Finder — роутер, который умеет назвать шаблон маршрута по методу и пути без обработки запроса (chi.Mux).
type Finder interface {
	Find(rctx *chi.Context, method, path string) string
}

// Middleware считает запросы без поиска маршрута для ответов, написанных до его разбора (см. MiddlewareFor).
func (m *Metrics) Middleware(next http.Handler) http.Handler { return m.MiddlewareFor(nil)(next) }

// MiddlewareFor считает запросы. Ставится первым в роутере (внутри только номер запроса), чтобы в счёт попали и отказы
// периметра, и ответы с ошибкой. Шаблон адреса читается после обработки: роутер заполняет его по ходу. Ответ, написанный
// промежуточным обработчиком раньше, чем роутер выбрал маршрут (проверка входа, ограничение частоты, ошибка базы при
// проверке сеанса), остался бы без шаблона; для него шаблон ищется у роутера по методу и пути (finder).
func (m *Metrics) MiddlewareFor(finder Finder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return m.middleware(next, RouteNamer{Finder: finder}) }
}

func (m *Metrics) middleware(next http.Handler, namer RouteNamer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		st := &requestState{m: m}
		defer func() {
			if !st.streaming.Load() {
				m.inFlight.Dec()
			}
		}()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestStateKey{}, st)))
		method, route := methodLabel(r.Method), namer.Label(r)
		m.requests.WithLabelValues(method, route, statusLabel(rec.code)).Inc()
		if !st.streaming.Load() {
			// Поток живёт минуты по замыслу: в задержках запросов и в числе «обрабатываемых сейчас» он только мешал бы.
			m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		}
	})
}

// requestState — состояние запроса, которое обработчик может изменить через MarkStreaming.
type requestState struct {
	m         *Metrics
	streaming atomic.Bool
}

type requestStateKey struct{}

// MarkStreaming сообщает, что запрос стал долгим потоком (срез 39): он больше не считается «обрабатываемым сейчас» и не
// попадает в гистограмму времени ответа (его число остаётся в `scibox_http_requests_total`). Повторный вызов безвреден.
func MarkStreaming(ctx context.Context) {
	if st, ok := ctx.Value(requestStateKey{}).(*requestState); ok && st.streaming.CompareAndSwap(false, true) {
		st.m.inFlight.Dec()
	}
}

// Наблюдения за потоком чата (chat.Observer, срез 39).

// StreamOpened — поток открыт.
func (m *Metrics) StreamOpened() { m.chatStreams.Inc() }

// StreamClosed — поток закрыт по причине reason после lived.
func (m *Metrics) StreamClosed(reason string, lived time.Duration) {
	m.chatStreams.Dec()
	m.chatClosed.WithLabelValues(label(reason)).Inc()
	m.chatLifetime.Observe(lived.Seconds())
}

// StreamRejected — в потоке отказано.
func (m *Metrics) StreamRejected(reason string) { m.chatRejected.WithLabelValues(label(reason)).Inc() }

// EventDispatched — копия обработала событие чата.
func (m *Metrics) EventDispatched(kind string) { m.chatEvents.WithLabelValues(label(kind), "dispatched").Inc() }

// EventDelivered — событие записано в поток получателя через late после записи в журнал.
func (m *Metrics) EventDelivered(kind string, late time.Duration) {
	m.chatEvents.WithLabelValues(label(kind), "delivered").Inc()
	m.chatDelivery.Observe(late.Seconds())
}

// ListenerState — соединение LISTEN поднято (true) или упало.
func (m *Metrics) ListenerState(up bool) {
	if up {
		m.chatListener.Set(1)
		return
	}
	m.chatListener.Set(0)
}

// CatchUp — добрано n событий журнала.
func (m *Metrics) CatchUp(n int) { m.chatCatchUp.Add(float64(n)) }

// JobFinished принимает итог прохода фоновой задачи (jobs.Observer).
func (m *Metrics) JobFinished(name string, _ bool, took time.Duration, err error) {
	result := "ok"
	switch {
	case errors.Is(err, context.Canceled):
		result = "interrupted"
	case err != nil:
		result = "failed"
	}
	m.jobRuns.WithLabelValues(label(name), result).Inc()
	m.jobDuration.WithLabelValues(label(name)).Observe(took.Seconds())
}

// JobPanicked считает панику в фоновой задаче (jobs.Observer).
func (m *Metrics) JobPanicked(string) { m.Panic("job") }

// Panic считает перехваченную панику: where — "http" или "job".
func (m *Metrics) Panic(where string) { m.panics.WithLabelValues(label(where)).Inc() }

// statusRecorder запоминает код ответа. Остальные возможности писателя (Flush, Hijack, ReadFrom, deadline) отдаются
// через Unwrap: http.ResponseController находит их у настоящего писателя.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.code == 0 && code >= 200 { // 1xx — промежуточные ответы, итог ещё впереди
		s.code = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RouteNamer даёт запросу метку route: шаблон маршрута chi. Путь запроса в метку не попадает никогда.
type RouteNamer struct {
	// Finder ищет шаблон по методу и пути, когда запрос завершился раньше, чем роутер выбрал маршрут; nil — не ищет.
	Finder Finder
}

// Label — метка route запроса.
func (n RouteNamer) Label(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); usable(p) {
			return label(p)
		}
	}
	// «/api/*» (или пусто): ответ написан до выбора маршрута или адреса нет. Роутер может сказать, какой маршрут
	// подошёл бы этому запросу, и тогда ошибка проверки входа или 429 попадут в счёт своего адреса.
	if n.Finder != nil {
		if p := n.Finder.Find(chi.NewRouteContext(), r.Method, r.URL.Path); usable(p) {
			return label(p)
		}
	}
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		return RouteUnknown
	}
	return RouteSite
}

// usable — шаблон настоящего маршрута: не пустой и не «подроутер, не нашедший адрес» («/api/*»).
func usable(pattern string) bool { return pattern != "" && !strings.HasSuffix(pattern, "/*") }

// RouteLabel — метка route без поиска у роутера.
func RouteLabel(r *http.Request) string { return RouteNamer{}.Label(r) }

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodOptions: true, http.MethodConnect: true, http.MethodTrace: true,
}

// methodLabel сводит выдуманные методы к OTHER: метод приходит от посетителя.
func methodLabel(m string) string {
	if knownMethods[m] {
		return m
	}
	return "OTHER"
}

func statusLabel(code int) string {
	if code < 100 || code > 599 {
		// Обработчик ничего не записал: net/http ответит 200.
		code = http.StatusOK
	}
	return strconv.Itoa(code)
}

// label приводит значение метки к допустимому: непустое, не длиннее 100 знаков, без управляющих знаков.
func label(s string) string {
	if s == "" {
		return "unknown"
	}
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '_'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 100 {
		return string(r[:100])
	}
	return s
}

// LogHandler считает записи журнала по уровню и передаёт их дальше. Ставится в цепочку обработчиков рядом с маской.
type LogHandler struct {
	next slog.Handler
	m    *Metrics
}

// NewLogHandler оборачивает next.
func (m *Metrics) NewLogHandler(next slog.Handler) *LogHandler { return &LogHandler{next: next, m: m} }

// Enabled передаёт вопрос дальше.
func (h *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle считает запись и передаёт её дальше.
func (h *LogHandler) Handle(ctx context.Context, r slog.Record) error {
	var level string
	switch {
	case r.Level >= slog.LevelError:
		level = "error"
	case r.Level >= slog.LevelWarn:
		level = "warn"
	case r.Level >= slog.LevelInfo:
		level = "info"
	default:
		level = "debug"
	}
	h.m.logs.WithLabelValues(level).Inc()
	return h.next.Handle(ctx, r)
}

// WithAttrs передаёт атрибуты дальше.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{next: h.next.WithAttrs(attrs), m: h.m}
}

// WithGroup передаёт группу дальше.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	return &LogHandler{next: h.next.WithGroup(name), m: h.m}
}

// ObserveQuery принимает итог запроса к базе от пула (dbpool.Observer): время всех запросов и счётчики медленных и
// остановленных по сроку с именем запроса.
func (m *Metrics) ObserveQuery(name string, took time.Duration, slow, timedOut bool) {
	m.queryDuration.Observe(took.Seconds())
	switch {
	case timedOut:
		m.timeouts.WithLabelValues(m.queryNames.label(name)).Inc()
	case slow:
		m.slowQueries.WithLabelValues(m.queryNames.label(name)).Inc()
	}
}

// boundedSet даёт метку первым max именам, остальным «other»: имена запросов приходят из кода, но защита от
// неожиданного (запрос без имени с текстом внутри) стоит одной карты.
type boundedSet struct {
	mu   sync.Mutex
	max  int
	seen map[string]bool
}

func newBoundedSet(maxNames int) *boundedSet {
	return &boundedSet{max: maxNames, seen: map[string]bool{}}
}

func (b *boundedSet) label(name string) string {
	name = label(name)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen[name] {
		return name
	}
	if len(b.seen) >= b.max {
		return "other"
	}
	b.seen[name] = true
	return name
}
