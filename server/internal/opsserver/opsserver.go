// Package opsserver — внутренний порт сервера и воркера (срез 26, T-25, D-173): проверки живости и готовности для
// оркестратора, метрики для Prometheus и pprof для разработчика. Это не часть сайта и не часть API: порт слушает
// отдельный адрес (по умолчанию петлю 127.0.0.1), у него свой набор адресов, и ни один из них не отдаётся на
// публичном порту.
//
// Критичная зона (docs/TESTING.md): pprof раскрывает внутренности процесса, метрики — устройство системы, поэтому
// порт защищён тремя вещами: адрес не из интернета (в бою проверяет config), общий секрет в `Authorization: Bearer`,
// если он задан, и выключенный по умолчанию в бою pprof. Проверки живости и готовности отвечают только «да» и «нет» и
// названием проверки, без текста ошибок.
package opsserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Check — одна проверка готовности.
type Check struct {
	// Name — название в ответе (`database`, `migrations`); не должно содержать ничего, кроме слова.
	Name string
	// Run возвращает ошибку, если проверка не пройдена. Текст ошибки наружу не отдаётся, только в журнал.
	Run func(ctx context.Context) error
}

// Config — настройки внутреннего порта.
type Config struct {
	// Token — общий секрет для /metrics и /debug/pprof; пусто — без секрета (петля на машине разработчика).
	Token string
	// Pprof включает /debug/pprof/.
	Pprof bool
	// Metrics отдаёт метрики Prometheus; nil — адреса /metrics нет.
	Metrics http.Handler
	// Checks — проверки готовности, выполняются на каждый запрос /readyz с общим сроком CheckTimeout.
	Checks []Check
	// CheckTimeout — срок на все проверки; по умолчанию 2 секунды.
	CheckTimeout time.Duration
	// Logger пишет смену состояния готовности.
	Logger *slog.Logger
}

// DefaultCheckTimeout — срок проверок готовности по умолчанию.
const DefaultCheckTimeout = 2 * time.Second

// Server — внутренний порт.
type Server struct {
	cfg      Config
	handler  http.Handler
	draining atomic.Bool
	mu       sync.Mutex
	srv      *http.Server
	ready    atomic.Int32 // 0 — ещё не проверялось, 1 — готов, 2 — не готов: чтобы писать в журнал только смену
}

// New собирает адреса порта.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		return nil, errors.New("opsserver: logger is required")
	}
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = DefaultCheckTimeout
	}
	seen := map[string]bool{}
	for _, c := range cfg.Checks {
		if c.Name == "" || strings.ContainsAny(c.Name, " \t\r\n:") || c.Run == nil || seen[c.Name] {
			return nil, fmt.Errorf("opsserver: check %q is not valid", c.Name)
		}
		seen[c.Name] = true
	}
	s := &Server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", s.livez)
	mux.HandleFunc("GET /readyz", s.readyz)
	if cfg.Metrics != nil {
		mux.Handle("GET /metrics", s.protected(cfg.Metrics))
	}
	if cfg.Pprof {
		// Адреса pprof задаются явно, без `import _ "net/http/pprof"`: тот регистрирует их в общем http.DefaultServeMux,
		// и они появились бы на любом сервере процесса, который его использует.
		mux.Handle("GET /debug/pprof/", s.protected(http.HandlerFunc(pprof.Index)))
		mux.Handle("GET /debug/pprof/cmdline", s.protected(http.HandlerFunc(pprof.Cmdline)))
		mux.Handle("GET /debug/pprof/profile", s.protected(http.HandlerFunc(pprof.Profile)))
		mux.Handle("GET /debug/pprof/symbol", s.protected(http.HandlerFunc(pprof.Symbol)))
		mux.Handle("POST /debug/pprof/symbol", s.protected(http.HandlerFunc(pprof.Symbol)))
		mux.Handle("GET /debug/pprof/trace", s.protected(http.HandlerFunc(pprof.Trace)))
	}
	s.handler = noStore(mux)
	return s, nil
}

// Handler — все адреса порта (для тестов и для встраивания).
func (s *Server) Handler() http.Handler { return s.handler }

// SetDraining отмечает, что процесс останавливают: /readyz начинает отвечать «не готов», и балансировщик перестаёт
// слать запросы, пока сервер дописывает текущие. /livez остаётся «жив»: процесс работает до выхода.
func (s *Server) SetDraining() { s.draining.Store(true) }

// Serve принимает соединения на ln, пока не вызван Shutdown.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Профиль процессора по умолчанию идёт 30 секунд: срок записи ответа с запасом. pprof сам отказывает в профиле
		// длиннее срока записи, поэтому предел 90 секунд же и есть предел `?seconds=`.
		WriteTimeout:   90 * time.Second,
		IdleTimeout:    time.Minute,
		MaxHeaderBytes: 16 << 10,
	}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown останавливает порт, дожидаясь текущих запросов.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) livez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// readyz выполняет проверки и отвечает 200, если все пройдены, и 503, если нет. В теле по строке на проверку:
// «database: ok». Текст ошибки уходит в журнал, и только когда состояние изменилось.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.CheckTimeout)
	defer cancel()
	var body strings.Builder
	ok := true
	for _, c := range s.cfg.Checks {
		if err := c.Run(ctx); err != nil {
			ok = false
			fmt.Fprintf(&body, "%s: fail\n", c.Name)
			s.noteFailure(ctx, c.Name, err)
			continue
		}
		fmt.Fprintf(&body, "%s: ok\n", c.Name)
	}
	if s.draining.Load() {
		ok = false
		body.WriteString("draining: yes\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !ok {
		s.setReady(ctx, false)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body.String()))
		return
	}
	s.setReady(ctx, true)
	_, _ = w.Write([]byte(body.String()))
}

// noteFailure пишет в журнал причину первой неудачи проверки после успеха; повторные неудачи тихие: оркестратор
// спрашивает каждые несколько секунд.
func (s *Server) noteFailure(ctx context.Context, name string, err error) {
	if s.ready.Load() != 2 {
		s.cfg.Logger.WarnContext(ctx, "readiness check failed", "check", name, "err", err)
	}
}

func (s *Server) setReady(ctx context.Context, ok bool) {
	next := int32(2)
	if ok {
		next = 1
	}
	prev := s.ready.Swap(next)
	switch {
	case ok && prev == 2:
		s.cfg.Logger.InfoContext(ctx, "readiness restored")
	case !ok && prev != 2 && s.draining.Load():
		s.cfg.Logger.InfoContext(ctx, "not ready: the process is shutting down")
	}
}

// protected требует общий секрет, если он задан.
func (s *Server) protected(next http.Handler) http.Handler {
	if s.cfg.Token == "" {
		return next
	}
	want := sha256.Sum256([]byte(s.cfg.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		have := sha256.Sum256([]byte(got))
		// Сравниваются хеши одинаковой длины: время не зависит от того, сколько знаков угадано.
		if !ok || subtle.ConstantTimeCompare(have[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ops"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// noStore: ответы порта никогда не кешируются.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
