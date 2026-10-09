package filework

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
)

var bg = testkit.BG

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// logBuffer собирает журнал задачи, чтобы тесты могли проверить, что о беде сказано.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

type env struct {
	*testkit.World
	tm    *testkit.Team
	notes *notifications.Service
	clock *clock
	svc   *Service
	log   *logBuffer
	// vacancy — опубликованная вакансия подразделения A.
	vacancy uuid.UUID
}

func testConfig() Config {
	return Config{ScanBatch: 5, Lease: 5 * time.Minute, MaxBatches: 20, DeleteBatch: 3, OrphanAge: 24 * time.Hour, MaxOrphans: 2, MaxOrphanShare: 0.1}
}

// newEnv чистит всё, что оставили другие тесты: проход берёт все файлы базы подряд.
func newEnv(t *testing.T) *env {
	t.Helper()
	k := testkit.NewWorld(t)
	for _, q := range []string{`DELETE FROM application_files`, `DELETE FROM blob_deletions`, `DELETE FROM notifications`, `DELETE FROM outbox`} {
		if _, err := testkit.Pool.Exec(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	e := &env{World: k, log: &logBuffer{}, clock: &clock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	e.notes = notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	e.svc = newService(testkit.Pool, k.Vault, e.notes, testConfig(), slog.New(slog.NewTextHandler(e.log, nil)), e.clock.Now)
	e.tm = k.Team()
	e.vacancy = e.tm.Published(&e.tm.UnitA.ID).ID
	return e
}

// application заводит отклик человека напрямую в базе.
func (e *env) application(who testkit.Person) uuid.UUID {
	e.T.Helper()
	id, err := dbgen.New(testkit.Pool).InsertApplication(bg, dbgen.InsertApplicationParams{
		VacancyID: e.vacancy, UserID: who.ID, CoverLetter: "Сопроводительное письмо", ContactEmail: who.Email, Profile: []byte(`{}`), Now: e.clock.Now(),
	})
	if err != nil {
		e.T.Fatal(err)
	}
	return id
}

// pendingFile кладёт объект в хранилище и заводит строку «на проверке».
func (e *env) pendingFile(app uuid.UUID, name string, data []byte) (uuid.UUID, string) {
	e.T.Helper()
	key := files.NewKey()
	if _, err := e.Store.Put(bg, key, bytes.NewReader(data)); err != nil {
		e.T.Fatal(err)
	}
	return e.row(app, "attachment", name, key, len(data)), key
}

func (e *env) row(app uuid.UUID, kind, name, key string, size int) uuid.UUID {
	e.T.Helper()
	var id uuid.UUID
	err := testkit.Pool.QueryRow(bg, `INSERT INTO application_files (application_id, kind, name, size, storage_key, sha256, scan_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7) RETURNING id`, app, kind, name, size, key, bytes.Repeat([]byte{1}, 32), e.clock.Now()).Scan(&id)
	if err != nil {
		e.T.Fatal(err)
	}
	return id
}

type fileRow struct {
	status   string
	key      *string
	sig      *string
	scanned  *time.Time
	attempts int
	next     *time.Time
}

func (e *env) file(id uuid.UUID) fileRow {
	e.T.Helper()
	var r fileRow
	err := testkit.Pool.QueryRow(bg, `SELECT scan_status, storage_key, scan_signature, scanned_at, scan_attempts, scan_next_at FROM application_files WHERE id = $1`, id).
		Scan(&r.status, &r.key, &r.sig, &r.scanned, &r.attempts, &r.next)
	if err != nil {
		e.T.Fatal(err)
	}
	return r
}

func (e *env) pass() Report {
	e.T.Helper()
	rep, err := e.svc.Pass(bg)
	if err != nil {
		e.T.Fatalf("pass: %v", err)
	}
	return rep
}

func pdfData(marker string) []byte {
	return append([]byte("%PDF-1.7\n% "+marker+"\n"), bytes.Repeat([]byte("p"), 200)...)
}

func infectedData() []byte { return append(pdfData("virus"), antivirus.EICAR()...) }

func notificationsOf(t *testing.T, who testkit.Person, kind string) []string {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT body || ' ' || link FROM notifications WHERE user_id = $1 AND kind = $2`, who.ID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// storeWrap оборачивает хранилище, чтобы тест мог подменить поведение отдельной операции.
type storeWrap struct {
	blobstore.Store
	get func(ctx context.Context, key string) (blobstore.Object, error)
	put func(ctx context.Context, key string, r io.Reader) (int64, error)
}

func (s storeWrap) Get(ctx context.Context, key string) (blobstore.Object, error) {
	if s.get != nil {
		return s.get(ctx, key)
	}
	return s.Store.Get(ctx, key)
}

func (s storeWrap) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	if s.put != nil {
		return s.put(ctx, key, r)
	}
	return s.Store.Put(ctx, key, r)
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func claimParams(now time.Time, limit int32, lease time.Duration) dbgen.ClaimFilesToScanParams {
	return dbgen.ClaimFilesToScanParams{Now: now, Lim: limit, LeaseUntil: now.Add(lease)}
}
