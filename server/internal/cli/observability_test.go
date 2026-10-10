package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/testdb"
)

// opsRun запускает команду так же, как Run в работе, и сообщает оба порта: публичный (для serve) и внутренний.
func opsRun(ctx context.Context, args []string, env map[string]string, onOps, onListen func(net.Addr)) result {
	var out, errb bytes.Buffer
	for k, v := range map[string]string{
		"SCIBOX_PRODUCT_CONFIG": productConfig, "SCIBOX_S3_BUCKET": testBucket,
		"SCIBOX_OPS_ADDR": "127.0.0.1:0", "SCIBOX_HTTP_ADDR": "127.0.0.1:0",
	} {
		if _, ok := env[k]; !ok {
			env[k] = v
		}
	}
	code := Run(ctx, args, Env{
		Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb,
		OnListen: onListen, OnOpsListen: onOps,
	})
	return result{code, out.String(), errb.String()}
}

func fetch(t *testing.T, method, url string, hdr map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

// Сервер с внутренним портом: проверки живости и готовности, метрики с шаблонами адресов, номер запроса в ответе и в
// журнале; ни один адрес внутреннего порта не отдаётся на публичном (T-25).
func TestServeInternalPortAndRequestIds(t *testing.T) {
	dbURL := testdb.Create(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ops, public net.Addr
	var wantID string
	checked := false
	r := opsRun(ctx, []string{"serve"}, map[string]string{
		"DATABASE_URL": dbURL, "SCIBOX_LOG_FORMAT": "json", "SCIBOX_ENV": "development", "SCIBOX_OPS_TOKEN": strings.Repeat("s", 32),
	}, func(a net.Addr) { ops = a }, func(a net.Addr) {
		defer cancel()
		public = a
		opsURL, pubURL := "http://"+ops.String(), "http://"+public.String()
		bearer := map[string]string{"Authorization": "Bearer " + strings.Repeat("s", 32)}

		// Публичный запрос: номер приходит в заголовке; присланный посетителем номер не принимается.
		code, hdr, _ := fetch(t, http.MethodGet, pubURL+"/api/health", map[string]string{"X-Request-Id": "attacker level=ERROR forged"})
		wantID = hdr.Get("X-Request-Id")
		if code != http.StatusOK || len(wantID) != 32 {
			t.Errorf("health: %d, request id %q", code, wantID)
		}
		fetch(t, http.MethodGet, pubURL+"/api/vacancies/00000000-0000-0000-0000-000000000001", nil)
		fetch(t, http.MethodGet, pubURL+"/api/vacancies/00000000-0000-0000-0000-000000000002", nil)

		// Внутренний порт.
		if code, _, body := fetch(t, http.MethodGet, opsURL+"/livez", nil); code != 200 || body != "ok\n" {
			t.Errorf("livez: %d %q", code, body)
		}
		if code, _, body := fetch(t, http.MethodGet, opsURL+"/readyz", nil); code != 200 || !strings.Contains(body, "database: ok") || !strings.Contains(body, "migrations: ok") {
			t.Errorf("readyz: %d %q", code, body)
		}
		if code, _, _ := fetch(t, http.MethodGet, opsURL+"/metrics", nil); code != http.StatusUnauthorized {
			t.Errorf("metrics without the token: %d", code)
		}
		code, _, metrics := fetch(t, http.MethodGet, opsURL+"/metrics", bearer)
		if code != 200 {
			t.Fatalf("metrics: %d", code)
		}
		for _, want := range []string{
			`scibox_http_requests_total{method="GET",route="/api/health",status="200"} 1`,
			`route="/api/vacancies/{id}"`,
			`scibox_http_request_duration_seconds_bucket`,
			`scibox_db_pool_connections{state="idle"}`, `scibox_db_pool_max_connections 20`,
			`scibox_mail_queue_messages{state="waiting"}`, `scibox_mail_queue_oldest_waiting_seconds`,
			`scibox_files_pending_scan`, `scibox_files_blob_deletions_queued`,
			`scibox_job_interval_seconds{job="outbox.send"}`, `scibox_job_last_success_timestamp_seconds{job="retention"}`,
			`scibox_breaker_state{name="crossref"} 0`, `scibox_breaker_state{name="mail"} 0`, `scibox_limiter_capacity{name="crossref"} 1`,
			`scibox_source_up{source="mail_queue"} 1`, `scibox_source_up{source="job_schedule"} 1`, `scibox_source_up{source="database"} 1`,
			`scibox_db_size_bytes`, `scibox_build_info{`, `scibox_panics_total{where="http"} 0`, `go_goroutines`,
		} {
			if !strings.Contains(metrics, want) {
				t.Errorf("metrics lack %q", want)
			}
		}
		// Номера объектов не попадают в метки: две разные вакансии — один ряд.
		if strings.Contains(metrics, "00000000-0000-0000-0000-00000000000") {
			t.Errorf("an object id reached a label")
		}
		if code, _, _ := fetch(t, http.MethodGet, opsURL+"/debug/pprof/", bearer); code != 200 {
			t.Errorf("pprof with the token: %d", code)
		}

		// Публичный порт не отдаёт ничего из внутреннего.
		for _, path := range []string{"/metrics", "/livez", "/readyz", "/debug/pprof/", "/debug/pprof/heap", "/api/metrics"} {
			code, _, body := fetch(t, http.MethodGet, pubURL+path, bearer)
			if code == 200 && (strings.Contains(body, "go_goroutines") || strings.Contains(body, "scibox_http") || strings.Contains(body, "database: ")) {
				t.Errorf("public %s answers with internal data", path)
			}
			if code != http.StatusNotFound {
				t.Errorf("public %s: %d, want 404", path, code)
			}
		}
		checked = true
	})
	if r.code != 0 || !checked {
		t.Fatalf("got %+v checked=%v", r, checked)
	}

	// В журнале строка запроса несёт тот же номер, что получил посетитель; подделка из заголовка в журнал не попала.
	if strings.Contains(r.stderr, "attacker") {
		t.Errorf("a request id from the client reached the log:\n%s", r.stderr)
	}
	var found bool
	for _, line := range strings.Split(r.stderr, "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec["msg"] != "request" || rec["path"] != "/api/health" {
			continue
		}
		found = true
		if rec["request_id"] != wantID || rec["route"] != "/api/health" || rec["service"] != "serve" || rec["env"] != "development" || rec["level"] != "INFO" {
			t.Errorf("request line = %v", rec)
		}
	}
	if !found {
		t.Errorf("no JSON line for the request:\n%s", r.stderr)
	}
}

// Воркер: живость, готовность и метрики задач на своём порту.
func TestWorkerInternalPort(t *testing.T) {
	dbURL := testdb.Create(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ops net.Addr
	ready := make(chan struct{})
	var failure string
	done := make(chan result, 1)
	go func() {
		done <- opsRun(ctx, []string{"worker"}, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_OPS_TOKEN": strings.Repeat("w", 32), "SCIBOX_LOG_FORMAT": "text"},
			func(a net.Addr) { ops = a; close(ready) }, nil)
	}()
	select {
	case <-ready:
	case <-time.After(20 * time.Second):
		t.Fatal("the worker did not open its internal port")
	}
	base := "http://" + ops.String()
	bearer := map[string]string{"Authorization": "Bearer " + strings.Repeat("w", 32)}
	if code, _, body := fetch(t, http.MethodGet, base+"/readyz", nil); code != 200 || !strings.Contains(body, "clock: ok") {
		failure = fmt.Sprintf("readyz: %d %q", code, body)
	}
	// Первый проход задач «в каждой копии» случается сразу: отметка последнего успеха появляется в метриках.
	var metrics string
	for range 100 {
		_, _, metrics = fetch(t, http.MethodGet, base+"/metrics", bearer)
		if strings.Contains(metrics, `scibox_job_runs_total{job="outbox.send",result="ok"}`) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, want := range []string{
		`scibox_job_runs_total{job="outbox.send",result="ok"}`, `scibox_job_duration_seconds_bucket{job="outbox.send"`,
		`scibox_job_local_last_success_timestamp_seconds{job="outbox.send"}`, `scibox_jobs_clock_skewed 0`,
		`scibox_job_last_success_timestamp_seconds{job="retention"}`, `scibox_build_info{go_version=`,
	} {
		if !strings.Contains(metrics, want) && failure == "" {
			failure = "worker metrics lack " + want
		}
	}
	cancel()
	r := <-done
	if failure != "" {
		t.Fatal(failure)
	}
	if r.code != 0 || !strings.Contains(r.stderr, "worker stopped") || !strings.Contains(r.stderr, "service=worker") {
		t.Fatalf("got %+v", r)
	}
}

// Настоящая ошибка сервера идёт по всей цепочке: 500 ответ → строка журнала с номером запроса → отчёт в GlitchTip с тем
// же номером и без личных данных → счётчик 500 и счётчик записей уровня error в метриках (T-25, срез 26).
func TestServerErrorReachesLogMetricsAndTracker(t *testing.T) {
	dbURL := testdb.Create(t, true)
	var mu sync.Mutex
	var reports []string
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reports = append(reports, r.URL.Path+" "+string(b))
		mu.Unlock()
	}))
	defer tracker.Close()
	dsn := strings.Replace(tracker.URL, "http://", "http://key@", 1) + "/1"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ops net.Addr
	var requestID, metrics string
	r := opsRun(ctx, []string{"serve"}, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_ERROR_DSN": dsn, "SCIBOX_LOG_FORMAT": "json"},
		func(a net.Addr) { ops = a }, func(a net.Addr) {
			defer cancel()
			// Ломаем схему под работающим сервером: запрос, который читает представление вакансии, получит ошибку базы.
			conn, err := pgx.Connect(context.Background(), dbURL)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.Close(context.Background()) }()
			if _, err := conn.Exec(context.Background(), `ALTER VIEW vacancy_view RENAME TO vacancy_view_gone`); err != nil {
				t.Error(err)
				return
			}
			code, hdr, body := fetch(t, http.MethodGet, "http://"+a.String()+"/api/vacancies/00000000-0000-0000-0000-000000000009", nil)
			requestID = hdr.Get("X-Request-Id")
			if code != http.StatusInternalServerError || strings.Contains(body, "vacancy_view_gone") || strings.Contains(body, "relation") {
				t.Errorf("answer: %d %s", code, body)
			}
			// Отчёт уходит в фоне.
			for range 100 {
				mu.Lock()
				n := len(reports)
				mu.Unlock()
				if n > 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			_, _, metrics = fetch(t, http.MethodGet, "http://"+ops.String()+"/metrics", nil)
		})
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) == 0 {
		t.Fatal("the tracker got no report")
	}
	report := reports[0]
	if !strings.HasPrefix(report, "/api/1/store/ ") || !strings.Contains(report, requestID) || !strings.Contains(report, "request failed") {
		t.Errorf("report: %s", report)
	}
	if strings.Contains(report, "vacancy_view_gone") {
		t.Errorf("the database error text reached the tracker: %s", report)
	}
	for _, want := range []string{`scibox_http_requests_total{method="GET",route="/api/vacancies/{id}",status="500"} 1`, `scibox_log_events_total{level="error"} 1`, `scibox_error_reports_total{result="sent"}`} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	// В журнале та же ошибка с тем же номером запроса.
	var inLog bool
	for _, line := range strings.Split(r.stderr, "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["level"] == "ERROR" && rec["request_id"] == requestID {
			inLog = true
		}
	}
	if !inLog {
		t.Errorf("no ERROR line with request id %s:\n%s", requestID, r.stderr)
	}
}

func TestErrorTestCommand(t *testing.T) {
	var got []string
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.URL.Path+" "+string(b))
	}))
	defer tracker.Close()
	dsn := strings.Replace(tracker.URL, "http://", "http://key@", 1) + "/5"
	r := run(context.Background(), []string{"error-test"}, map[string]string{"SCIBOX_ERROR_DSN": dsn}, nil)
	if r.code != 0 || !strings.Contains(r.stdout, "принят") || len(got) != 1 || !strings.HasPrefix(got[0], "/api/5/store/ ") {
		t.Fatalf("got %+v, reports %v", r, got)
	}
	// Без адреса: понятный отказ.
	r = run(context.Background(), []string{"error-test"}, map[string]string{}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "SCIBOX_ERROR_DSN") {
		t.Fatalf("got %+v", r)
	}
	// Сервис отказывает: код 1 и причина.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer refusing.Close()
	r = run(context.Background(), []string{"error-test"}, map[string]string{"SCIBOX_ERROR_DSN": strings.Replace(refusing.URL, "http://", "http://key@", 1) + "/5"}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("got %+v", r)
	}
}

// Занятый внутренний порт останавливает запуск понятной ошибкой, а «off» его выключает.
func TestInternalPortBusyAndOff(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	r := opsRun(context.Background(), []string{"serve"}, map[string]string{"DATABASE_URL": testdb.Create(t, true), "SCIBOX_OPS_ADDR": ln.Addr().String()}, nil, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "SCIBOX_OPS_ADDR") {
		t.Fatalf("busy: %+v", r)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r = opsRun(ctx, []string{"serve"}, map[string]string{"DATABASE_URL": testdb.Create(t, true), "SCIBOX_OPS_ADDR": "off"}, func(net.Addr) {
		t.Error("the internal port opened although it is off")
	}, func(net.Addr) { cancel() })
	if r.code != 0 || !strings.Contains(r.stderr, "internal port is off") {
		t.Fatalf("off: %+v", r)
	}
}
