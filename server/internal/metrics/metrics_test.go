package metrics_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/metrics"
)

// router — маленькая копия настоящего: шаблоны с параметрами, подроутер, ответ без записи, паника не нужна.
func router(m *metrics.Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Route("/api", func(r chi.Router) {
		r.Get("/vacancies/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		r.Post("/applications", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
		r.Get("/broken", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
		r.Get("/silent", func(http.ResponseWriter, *http.Request) {})
		r.Get("/early", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusAccepted)
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("<html>"))
	})
	return r
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func series(t *testing.T, m *metrics.Metrics, name string) map[string]float64 {
	t.Helper()
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, s := range f.GetMetric() {
			var labels []string
			for _, l := range s.GetLabel() {
				labels = append(labels, l.GetName()+"="+l.GetValue())
			}
			v := s.GetCounter().GetValue()
			if f.GetType().String() == "HISTOGRAM" {
				v = float64(s.GetHistogram().GetSampleCount())
			}
			out[strings.Join(labels, ",")] = v
		}
	}
	return out
}

func TestRequestsAreCountedByRouteTemplateMethodAndStatus(t *testing.T) {
	m := metrics.New("serve", "test")
	h := router(m)
	do(h, http.MethodGet, "/api/vacancies/6e1a7f0e-3b0a-4f6e-9a77-0b8d3a5f1c11")
	do(h, http.MethodGet, "/api/vacancies/another-id")
	do(h, http.MethodPost, "/api/applications")
	do(h, http.MethodGet, "/api/broken")
	do(h, http.MethodGet, "/api/silent")
	do(h, http.MethodGet, "/api/early")
	do(h, http.MethodGet, "/api/no/such/thing")
	do(h, http.MethodGet, "/vacancies/any-page")

	got := series(t, m, "scibox_http_requests_total")
	want := map[string]float64{
		"method=GET,route=/api/vacancies/{id},status=200": 2,
		"method=POST,route=/api/applications,status=201":  1,
		"method=GET,route=/api/broken,status=500":         1,
		"method=GET,route=/api/silent,status=200":         1,
		"method=GET,route=/api/early,status=202":          1,
		"method=GET,route=api_unknown,status=404":         1,
		"method=GET,route=site,status=200":                1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v\nall: %v", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d series, want %d: %v", len(got), len(want), got)
	}
	if n := series(t, m, "scibox_http_request_duration_seconds")["method=GET,route=/api/vacancies/{id}"]; n != 2 {
		t.Errorf("duration samples = %v, want 2", n)
	}
}

// Посетитель не раздувает число рядов, меняя адрес или метод (T-25: «метки с адресами объектов взрывают число рядов»).
func TestRouteLabelNeverFollowsThePath(t *testing.T) {
	m := metrics.New("serve", "test")
	h := router(m)
	for i := range 500 {
		do(h, http.MethodGet, fmt.Sprintf("/api/vacancies/%d", i))
		do(h, http.MethodGet, fmt.Sprintf("/api/nothing-here-%d", i))
		do(h, http.MethodGet, fmt.Sprintf("/random-page-%d", i))
		do(h, "BREW"+fmt.Sprint(i), "/api/vacancies/1")
	}
	got := series(t, m, "scibox_http_requests_total")
	if len(got) > 4 {
		t.Fatalf("%d series after 2000 different requests: %v", len(got), got)
	}
	if got["method=OTHER,route=/api/vacancies/{id},status=405"] == 0 && got["method=OTHER,route=api_unknown,status=404"] == 0 &&
		got["method=OTHER,route=api_unknown,status=405"] == 0 {
		t.Errorf("unknown methods are not folded into OTHER: %v", got)
	}
	for k := range got {
		if strings.Contains(k, "BREW") || strings.Contains(k, "nothing-here") || strings.Contains(k, "random-page") {
			t.Errorf("a label follows the request: %s", k)
		}
	}
}

func TestInFlightGaugeAndPanicCounter(t *testing.T) {
	m := metrics.New("worker", "1.2.3")
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := m.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	go func() {
		defer close(done)
		do(h, http.MethodGet, "/api/slow")
	}()
	<-started
	if got := gaugeValue(t, m, "scibox_http_requests_in_flight"); got != 1 {
		t.Errorf("in flight = %v, want 1", got)
	}
	close(release)
	<-done
	if got := gaugeValue(t, m, "scibox_http_requests_in_flight"); got != 0 {
		t.Errorf("in flight after = %v, want 0", got)
	}

	m.Panic("http")
	m.Panic("job")
	m.Panic("job")
	m.Panic("weird\nvalue")
	got := series(t, m, "scibox_panics_total")
	if got["where=http"] != 1 || got["where=job"] != 2 || got["where=weird_value"] != 1 {
		t.Errorf("panics = %v", got)
	}
	info := series(t, m, "scibox_build_info")
	if len(info) != 1 {
		t.Errorf("build info = %v", info)
	}
}

// gaugeValue читает значение гейджа без меток по имени из реестра.
func gaugeValue(t *testing.T, m *metrics.Metrics, name string) float64 {
	t.Helper()
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestLogHandlerCountsRecordsByLevel(t *testing.T) {
	m := metrics.New("serve", "test")
	var out bytes.Buffer
	logger := slog.New(m.NewLogHandler(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))).With("a", "b").WithGroup("g")
	logger.Debug("dropped by level")
	logger.Info("one")
	logger.Warn("two")
	logger.Warn("three")
	logger.Error("four", "err", context.DeadlineExceeded)
	got := series(t, m, "scibox_log_events_total")
	want := map[string]float64{"level=debug": 0, "level=info": 1, "level=warn": 2, "level=error": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if !strings.Contains(out.String(), "a=b") || !strings.Contains(out.String(), "g.err=") {
		t.Errorf("records did not reach the next handler intact:\n%s", out.String())
	}
}

func TestQueryObserverCountsSlowAndTimedOutByName(t *testing.T) {
	m := metrics.New("serve", "test")
	m.ObserveQuery("GetVacancy", 5*time.Millisecond, false, false)
	m.ObserveQuery("SearchVacancies", 300*time.Millisecond, true, false)
	m.ObserveQuery("SearchVacancies", 400*time.Millisecond, true, false)
	m.ObserveQuery("ListCandidates", 10*time.Second, true, true)
	if got := series(t, m, "scibox_db_slow_queries_total"); got["query=SearchVacancies"] != 2 || len(got) != 1 {
		t.Errorf("slow = %v", got)
	}
	if got := series(t, m, "scibox_db_query_timeouts_total"); got["query=ListCandidates"] != 1 || len(got) != 1 {
		t.Errorf("timeouts = %v", got)
	}
	families, _ := m.Registry.Gather()
	var count uint64
	for _, f := range families {
		if f.GetName() == "scibox_db_query_duration_seconds" {
			count = f.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	if count != 4 {
		t.Errorf("duration samples = %d, want 4", count)
	}
}

// Запрос без имени, чей текст каждый раз новый, не раздувает метки: сверх предела всё идёт в «other».
func TestQueryNamesAreBounded(t *testing.T) {
	m := metrics.New("serve", "test")
	for i := range 1000 {
		m.ObserveQuery(fmt.Sprintf("SELECT * FROM t WHERE id = %d", i), time.Second, true, false)
	}
	got := series(t, m, "scibox_db_slow_queries_total")
	if len(got) > 301 || got["query=other"] != 700 {
		t.Errorf("series = %d, other = %v", len(got), got["query=other"])
	}
}

func TestRouteLabelOutsideARouter(t *testing.T) {
	for path, want := range map[string]string{"/api": metrics.RouteUnknown, "/api/x": metrics.RouteUnknown, "/apix": metrics.RouteSite, "/": metrics.RouteSite} {
		if got := metrics.RouteLabel(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

// Ответ, написанный промежуточным обработчиком подроутера раньше выбора маршрута (проверка входа, 429, ошибка базы),
// считается по адресу, которому он предназначался, а не теряется в «неизвестных».
func TestResponsesWrittenBeforeRoutingAreCountedByTheirRoute(t *testing.T) {
	m := metrics.New("serve", "test")
	r := chi.NewRouter()
	r.Use(m.MiddlewareFor(r))
	r.Route("/api", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("X-Abort") != "" {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				next.ServeHTTP(w, req)
			})
		})
		r.Get("/vacancies/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	})
	for _, target := range []string{"/api/vacancies/1", "/api/vacancies/2", "/api/no/such"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("X-Abort", "1")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	got := series(t, m, "scibox_http_requests_total")
	if got["method=GET,route=/api/vacancies/{id},status=429"] != 2 || got["method=GET,route=api_unknown,status=429"] != 1 || len(got) != 2 {
		t.Fatalf("series = %v", got)
	}
}

func TestJobObserverMethods(t *testing.T) {
	m := metrics.New("worker", "test")
	m.JobFinished("retention", true, 2*time.Second, nil)
	m.JobFinished("retention", true, time.Second, errors.New("down"))
	m.JobFinished("outbox.send", false, time.Millisecond, fmt.Errorf("stopped: %w", context.Canceled))
	m.JobPanicked("retention")
	runs := series(t, m, "scibox_job_runs_total")
	for k, want := range map[string]float64{
		"result=ok,task=retention": 1, "result=failed,task=retention": 1, "result=interrupted,task=outbox.send": 1,
	} {
		if runs[k] != want {
			t.Errorf("%s = %v, want %v (all: %v)", k, runs[k], want, runs)
		}
	}
	if got := series(t, m, "scibox_job_duration_seconds"); got["task=retention"] != 2 || got["task=outbox.send"] != 1 {
		t.Errorf("durations = %v", got)
	}
	if got := series(t, m, "scibox_panics_total"); got["where=job"] != 1 {
		t.Errorf("panics = %v", got)
	}
}

// Расширенные возможности писателя (Flush, дедлайны) доходят до настоящего писателя через обёртку счёта запросов.
func TestWrapperKeepsFlushingAvailable(t *testing.T) {
	m := metrics.New("serve", "test")
	flushed := false
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("part"))
		flushed = http.NewResponseController(w).Flush() == nil
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stream", nil))
	if !flushed || !rec.Flushed {
		t.Fatalf("flushed = %v, recorder flushed = %v", flushed, rec.Flushed)
	}
}

func TestDebugRecordsAreCountedAndLabelsAreSafe(t *testing.T) {
	m := metrics.New("serve", "test")
	logger := slog.New(m.NewLogHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	logger.Debug("noise")
	if got := series(t, m, "scibox_log_events_total"); got["level=debug"] != 1 {
		t.Errorf("debug = %v", got)
	}
	// Пустое имя запроса, управляющие знаки и длинная строка в метке.
	m.ObserveQuery("", time.Second, true, false)
	m.ObserveQuery("bad\x00name\n", time.Second, true, false)
	m.ObserveQuery(strings.Repeat("я", 300), time.Second, true, false)
	got := series(t, m, "scibox_db_slow_queries_total")
	if got["query=unknown"] != 1 || got["query=bad_name_"] != 1 || got["query="+strings.Repeat("я", 100)] != 1 {
		t.Errorf("slow = %v", got)
	}
}

// BenchmarkMiddleware: цена счёта одного запроса (docs/SCALING.md): запись в счётчик и гистограмму плюс поиск
// метки.
func BenchmarkMiddleware(b *testing.B) {
	m := metrics.New("serve", "bench")
	h := router(m)
	req := httptest.NewRequest(http.MethodGet, "/api/vacancies/6e1a7f0e-3b0a-4f6e-9a77-0b8d3a5f1c11", nil)
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

// BenchmarkBare — тот же роутер без счёта: разница между двумя и есть цена метрик.
func BenchmarkBare(b *testing.B) {
	r := chi.NewRouter()
	r.Get("/api/vacancies/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	req := httptest.NewRequest(http.MethodGet, "/api/vacancies/6e1a7f0e-3b0a-4f6e-9a77-0b8d3a5f1c11", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
}
