// Package httpguard — периметр HTTP-сервера (срез 19): сроки соединений, заголовки защиты, настоящий адрес
// человека за прокси, лимит тела запроса на каждом адресе и ограничение частоты (T-04…T-07, T-09).
// Критичная зона: ошибка здесь открывает сервер для перегрузки или подмены адреса.
package httpguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
)

// DefaultBody — лимит тела запроса для любого адреса, которого нет в таблице: самые большие формы
// сайта (профиль, вакансия) в десятки раз меньше.
const DefaultBody int64 = 1 << 20

// Route — особые правила одного адреса. Pattern — шаблон роутера chi целиком:
// "/api/applications/{id}/files/{fileId}". Косая черта в конце не важна: chi.Walk пишет корень поддерева
// как "/api/organizations/", а поиск адреса запроса — как "/api/organizations".
type Route struct {
	Method  string
	Pattern string
	// Body — лимит тела в байтах; 0 значит DefaultBody (не «без лимита»).
	Body int64
	// Slow — долгая передача (загрузка или выдача файлов): сроки чтения и записи Timeouts.Transfer
	// вместо обычных.
	Slow bool
	// Limit — ограничение частоты; nil — без ограничения.
	Limit *Policy
}

// Policy — ограничение частоты: гость считается по адресу, вошедший по аккаунту.
// Name — вид счётчика; адреса с одной политикой делят один счётчик.
type Policy struct {
	Name  string
	Guest Rate
	User  Rate
}

// Rate — не больше Requests запросов за окно Per.
type Rate struct {
	Requests int
	Per      time.Duration
}

// Окна счётчиков: от секунды до часа. Старые счётчики удаляются через KeepCounters.
const (
	minWindow = time.Second
	maxWindow = time.Hour
	// KeepCounters — сколько хранить счётчик после начала его окна.
	KeepCounters = 2 * maxWindow
)

// Who сообщает номер вошедшего человека; ok == false — гость.
type Who func(r *http.Request) (id string, ok bool)

// Config — настройки периметра.
type Config struct {
	Proxies Proxies
	// HTTPS — сайт открывается по https (SCIBOX_PUBLIC_URL): тогда ставится HSTS.
	HTTPS  bool
	Routes []Route
	// Store — где живут счётчики; nil — в памяти процесса (годится для одной копии сервера и тестов).
	Store  Store
	Logger *slog.Logger
	// Now — часы; nil — time.Now.
	Now func() time.Time
	// Transfer — срок долгой передачи (Route.Slow); 0 и меньше — DefaultTimeouts().Transfer.
	Transfer time.Duration
}

// Finder — роутер, который умеет назвать шаблон адреса для метода и пути (chi.Mux).
type Finder interface {
	Find(rctx *chi.Context, method, path string) string
}

// Guard — периметр. Создаётся New, подключается двумя промежуточными обработчиками:
// Perimeter — первым во всём роутере, Throttle — после проверки входа.
type Guard struct {
	cfg    Config
	router Finder
	routes map[string]Route
}

// New проверяет таблицу адресов и собирает периметр. router — тот роутер, в котором стоит Perimeter:
// по нему Guard узнаёт шаблон адреса запроса.
func New(cfg Config, router Finder) (*Guard, error) {
	if router == nil {
		return nil, errors.New("httpguard: router is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("httpguard: logger is required")
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.Transfer = positive(cfg.Transfer, DefaultTimeouts().Transfer)
	g := &Guard{cfg: cfg, router: router, routes: make(map[string]Route, len(cfg.Routes))}
	policies := map[string]Policy{}
	for _, rt := range cfg.Routes {
		if err := checkRoute(rt); err != nil {
			return nil, err
		}
		key := routeKey(rt.Method, rt.Pattern)
		if _, dup := g.routes[key]; dup {
			return nil, fmt.Errorf("httpguard: route %s is listed twice", key)
		}
		if rt.Limit != nil {
			if prev, seen := policies[rt.Limit.Name]; seen && prev != *rt.Limit {
				return nil, fmt.Errorf("httpguard: policy %q has two different sets of limits", rt.Limit.Name)
			}
			policies[rt.Limit.Name] = *rt.Limit
		}
		g.routes[key] = rt
	}
	return g, nil
}

func checkRoute(rt Route) error {
	switch rt.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("httpguard: route %q: unknown method %q", rt.Pattern, rt.Method)
	}
	if len(rt.Pattern) == 0 || rt.Pattern[0] != '/' {
		return fmt.Errorf("httpguard: route %s %q: pattern must start with /", rt.Method, rt.Pattern)
	}
	if rt.Body < 0 {
		return fmt.Errorf("httpguard: route %s %s: negative body limit", rt.Method, rt.Pattern)
	}
	if p := rt.Limit; p != nil {
		if p.Name == "" {
			return fmt.Errorf("httpguard: route %s %s: policy without a name", rt.Method, rt.Pattern)
		}
		for who, r := range map[string]Rate{"guest": p.Guest, "user": p.User} {
			if r.Requests < 1 || r.Per < minWindow || r.Per > maxWindow {
				return fmt.Errorf("httpguard: policy %q: %s rate needs at least 1 request per %v…%v, got %d per %v",
					p.Name, who, minWindow, maxWindow, r.Requests, r.Per)
			}
		}
	}
	return nil
}

// Routes возвращает таблицу особых адресов (для проверки, что каждый из них есть в роутере).
func (g *Guard) Routes() []Route {
	return g.cfg.Routes
}

// routeKey — ключ таблицы: метод и шаблон без косой черты в конце.
func routeKey(method, pattern string) string {
	if len(pattern) > 1 {
		pattern = strings.TrimSuffix(pattern, "/")
	}
	return method + " " + pattern
}

type routeCtxKey struct{}

// route — правила адреса запроса: из контекста (их положил Perimeter) или из таблицы.
func (g *Guard) route(r *http.Request) Route {
	if rt, ok := r.Context().Value(routeCtxKey{}).(Route); ok {
		return rt
	}
	pattern := g.router.Find(chi.NewRouteContext(), r.Method, r.URL.Path)
	if pattern == "" {
		return Route{}
	}
	return g.routes[routeKey(r.Method, pattern)]
}

// Perimeter — первый промежуточный обработчик всего роутера: адрес человека, заголовки защиты,
// лимит тела, сроки долгих передач.
func (g *Guard) Perimeter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = withClient(r, g.cfg.Proxies.clientAddr(r))
		setHeaders(w.Header(), r.URL.Path, g.cfg.HTTPS)

		rt := g.route(r)
		r = r.WithContext(context.WithValue(r.Context(), routeCtxKey{}, rt))

		limit := rt.Body
		if limit == 0 {
			limit = DefaultBody
		}
		if r.ContentLength > limit {
			// Тело больше лимита отвергается сразу, ещё до чтения: соединение после ответа закроется.
			apierr.WriteError(w, http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, "Запрос слишком большой")
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		if rt.Slow {
			extendDeadlines(w, time.Now().Add(g.cfg.Transfer))
		}
		next.ServeHTTP(w, r)
	})
}

// extendDeadlines продлевает сроки соединения для долгой передачи. В тестах с httptest.ResponseRecorder
// продлевать нечего (http.ErrNotSupported); у настоящего соединения HTTP/1.1 это всегда работает.
func extendDeadlines(w http.ResponseWriter, until time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(until)
	_ = rc.SetWriteDeadline(until)
}

// Throttle ограничивает частоту запросов к адресам с политикой. Ставится после проверки входа:
// who отличает вошедшего от гостя. Сбой хранилища — ошибка 500, а не пропуск без счёта (I-09).
func (g *Guard) Throttle(who Who) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := g.route(r).Limit
			if p == nil {
				next.ServeHTTP(w, r)
				return
			}
			rate, key := p.Guest, guestKey(clientIP(r))
			if who != nil {
				if id, ok := who(r); ok {
					rate, key = p.User, "user:"+id
				}
			}
			now := g.cfg.Now()
			window := now.Truncate(rate.Per)
			hits, err := g.cfg.Store.Hit(r.Context(), p.Name, key, window)
			if err != nil {
				apierr.WriteInternal(w, r, g.cfg.Logger, "rate limit", err)
				return
			}
			if hits > rate.Requests {
				secs := max(1, int(math.Ceil(window.Add(rate.Per).Sub(now).Seconds())))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				apierr.WriteJSON(w, http.StatusTooManyRequests, apierr.ErrorBody{Error: apierr.ErrorDetail{
					Code: apierr.CodeRateLimited, Message: "Слишком много запросов. Подождите немного и попробуйте снова", RetryAfter: secs,
				}})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
