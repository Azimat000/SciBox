package retention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/testkit"
)

var bg = testkit.BG

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clock — часы, отстающие от настоящих не больше чем на микросекунду (PostgreSQL хранит микросекунды).
func clock() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func service(t *testing.T, cfg Config, now time.Time) *Service {
	t.Helper()
	return newService(testkit.Pool, cfg, quiet(), func() time.Time { return now })
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testkit.Pool.Exec(bg, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func newUser(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := fmt.Sprintf("ret%d@example.ru", testkit.Seq())
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO users (email, display_name, password_hash, privacy_consent_at, privacy_policy_version, email_confirmed_at, created_at, updated_at)
		VALUES ($1, 'Тест', 'x', now(), 'v', now(), now(), now()) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// clean убирает всё, что чистка могла бы подобрать: база общая на весь пакет, а проход стирает всё просроченное, а не
// только данные своего теста. Нужна тестам, которые считают удалённое.
func clean(t *testing.T) {
	t.Helper()
	exec(t, `DELETE FROM notifications WHERE created_at < now() - interval '30 days' OR read_at IS NOT NULL`)
	exec(t, `DELETE FROM users WHERE email_confirmed_at IS NULL AND deleted_at IS NULL AND updated_at < now() - interval '1 day'`)
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	if _, err := tx.Exec(bg, `SELECT set_config('scibox.audit_purge', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(bg, `DELETE FROM audit_log WHERE at < now() - interval '91 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(bg, `UPDATE audit_log SET ip = NULL WHERE ip IS NOT NULL AND at < now() - interval '181 days'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(bg); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyNormalized(t *testing.T) {
	def, low := DefaultPolicy(), floors()
	tests := []struct {
		name string
		in   Policy
		want Policy
	}{
		{"zero policy means the defaults, not no limit", Policy{}, def},
		{"negative values mean the defaults", Policy{ReadNotifications: -time.Hour, UnreadNotifications: -1, AuditLogYears: -3, AuditAddress: -day, UnconfirmedAccounts: -day}, def},
		{"too short values are raised to the floor", Policy{ReadNotifications: time.Minute, UnreadNotifications: time.Hour, AuditLogYears: 0, AuditAddress: day, UnconfirmedAccounts: time.Hour}, Policy{
			ReadNotifications: low.ReadNotifications, UnreadNotifications: low.UnreadNotifications, AuditLogYears: def.AuditLogYears, AuditAddress: low.AuditAddress, UnconfirmedAccounts: low.UnconfirmedAccounts}},
		{"exactly the floor stays", low, low},
		{"longer values stay", Policy{ReadNotifications: 400 * day, UnreadNotifications: 800 * day, AuditLogYears: 5, AuditAddress: 900 * day, UnconfirmedAccounts: 30 * day}, Policy{
			ReadNotifications: 400 * day, UnreadNotifications: 800 * day, AuditLogYears: 5, AuditAddress: 900 * day, UnconfirmedAccounts: 30 * day}},
		{"one year of journal is allowed", Policy{AuditLogYears: 1}, Policy{ReadNotifications: def.ReadNotifications, UnreadNotifications: def.UnreadNotifications, AuditLogYears: 1, AuditAddress: def.AuditAddress, UnconfirmedAccounts: def.UnconfirmedAccounts}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalized(); got != tt.want {
				t.Errorf("Normalized() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDefaultsAreTheDecisionsOfTheOwner(t *testing.T) {
	p := DefaultPolicy()
	if p.ReadNotifications != 90*day || p.UnreadNotifications != 365*day || p.AuditLogYears != 3 {
		t.Errorf("D-151 says: read 90 days, unread a year, journal 3 years; got %+v", p)
	}
	if got := newService(testkit.Pool, Config{}, quiet(), time.Now).cfg; got.Batch != 1000 || got.MaxBatches != 200 || got.Policy != p {
		t.Errorf("an empty config must turn into the defaults, got %+v", got)
	}
}

// notification кладёт уведомление и возвращает его номер.
func notification(t *testing.T, user uuid.UUID, created time.Time, readAt *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO notifications (user_id, kind, title, created_at, read_at) VALUES ($1, 'test', 'тест', $2, $3) RETURNING id`, user, created, readAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func exists(t *testing.T, table string, id uuid.UUID) bool {
	t.Helper()
	return testkit.Count(t, "SELECT count(*) FROM "+table+" WHERE id = $1", id) == 1
}

func TestNotificationsAreKeptForTheirTerm(t *testing.T) {
	now := clock()
	svc := service(t, Config{}, now)
	user := newUser(t)
	p := DefaultPolicy()
	readCut, unreadCut := now.Add(-p.ReadNotifications), now.Add(-p.UnreadNotifications)
	at := func(tm time.Time) *time.Time { return &tm }
	cases := []struct {
		name    string
		created time.Time
		read    *time.Time
		gone    bool
	}{
		{"read exactly at the cutoff stays", now.AddDate(-1, 0, 0), at(readCut), false},
		{"read a second before the cutoff goes", now.AddDate(-1, 0, 0), at(readCut.Add(-time.Second)), true},
		{"read a second after the cutoff stays", now.AddDate(-1, 0, 0), at(readCut.Add(time.Second)), false},
		{"old but read yesterday stays", now.AddDate(-3, 0, 0), at(now.Add(-day)), false},
		{"unread exactly at the cutoff stays", unreadCut, nil, false},
		{"unread a second before the cutoff goes", unreadCut.Add(-time.Second), nil, true},
		{"unread for 100 days stays (older than the read term, younger than the unread term)", now.Add(-100 * day), nil, false},
		{"fresh unread stays", now, nil, false},
	}
	ids := make([]uuid.UUID, len(cases))
	for i, c := range cases {
		ids[i] = notification(t, user, c.created, c.read)
	}
	report, err := svc.Pass(bg)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := !exists(t, "notifications", ids[i]); got != c.gone {
				t.Errorf("removed = %v, want %v", got, c.gone)
			}
		})
	}
	if report.ReadNotifications != 1 || report.UnreadNotifications != 1 {
		t.Errorf("report = %+v, want one read and one unread notification removed", report)
	}
	again, err := svc.Pass(bg)
	if err != nil || again.Total() != 0 {
		t.Errorf("a second pass must find nothing: %+v %v", again, err)
	}
}

// auditAt кладёт запись журнала с нужным временем и адресом.
func auditAt(t *testing.T, actor uuid.UUID, at time.Time, ip string) int64 {
	t.Helper()
	var id int64
	var addr *string
	if ip != "" {
		addr = &ip
	}
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO audit_log (at, actor_id, action, ip) VALUES ($1, $2, 'account.login', $3) RETURNING id`, at, actor, addr).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func auditExists(t *testing.T, id int64) (found bool, ip *string) {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT ip FROM audit_log WHERE id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		found = true
		if err := rows.Scan(&ip); err != nil {
			t.Fatal(err)
		}
	}
	return found, ip
}

func TestAuditLogIsKeptForThreeYears(t *testing.T) {
	now := clock()
	svc := service(t, Config{}, now)
	actor := newUser(t)
	cut := now.AddDate(-3, 0, 0)
	cases := []struct {
		name string
		at   time.Time
		gone bool
	}{
		{"exactly three years stays", cut, false},
		{"a second older than three years goes", cut.Add(-time.Second), true},
		{"a second younger than three years stays", cut.Add(time.Second), false},
		{"two years stays", now.AddDate(-2, 0, 0), false},
		{"ten years goes", now.AddDate(-10, 0, 0), true},
		{"yesterday stays", now.Add(-day), false},
	}
	ids := make([]int64, len(cases))
	for i, c := range cases {
		ids[i] = auditAt(t, actor, c.at, "")
	}
	report, err := svc.Pass(bg)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if found, _ := auditExists(t, ids[i]); found == c.gone {
				t.Errorf("found = %v, want %v", found, !c.gone)
			}
		})
	}
	if report.AuditLog < 2 {
		t.Errorf("report = %+v, want at least the two old records", report)
	}
}

func TestAuditAddressesAreDroppedAfterHalfAYear(t *testing.T) {
	now := clock()
	svc := service(t, Config{}, now)
	actor := newUser(t)
	cut := now.Add(-DefaultPolicy().AuditAddress)
	old := auditAt(t, actor, cut.Add(-time.Second), "203.0.113.5")
	edge := auditAt(t, actor, cut, "203.0.113.6")
	fresh := auditAt(t, actor, now.Add(-day), "203.0.113.7")
	oldNoAddress := auditAt(t, actor, cut.Add(-time.Hour), "")
	if _, err := svc.Pass(bg); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		id     int64
		wantIP bool
	}{{"older than the term loses the address", old, false}, {"exactly at the term keeps it", edge, true}, {"fresh keeps it", fresh, true}, {"no address stays none", oldNoAddress, false}} {
		found, ip := auditExists(t, c.id)
		if !found {
			t.Fatalf("%s: the record itself must stay", c.name)
		}
		if (ip != nil) != c.wantIP {
			t.Errorf("%s: address kept = %v, want %v", c.name, ip != nil, c.wantIP)
		}
	}
	var action string
	var at time.Time
	if err := testkit.Pool.QueryRow(bg, `SELECT action, at FROM audit_log WHERE id = $1`, old).Scan(&action, &at); err != nil || action != "account.login" || !at.Equal(cut.Add(-time.Second)) {
		t.Errorf("only the address may change: %q %v %v", action, at, err)
	}
	if again, err := svc.Pass(bg); err != nil || again.AuditAddresses != 0 {
		t.Errorf("addresses are dropped once: %+v %v", again, err)
	}
}

// Пол в триггере базы не зависит от настройки в коде: даже прямой запрос не стирает свежую историю.
func TestAuditTriggerProtectsRecentHistoryWhateverTheCodeSays(t *testing.T) {
	now := clock()
	actor := newUser(t)
	young := auditAt(t, actor, now.Add(-89*day), "")
	older := auditAt(t, actor, now.Add(-91*day), "")
	del := func(announce bool, id int64) error {
		tx, err := testkit.Pool.Begin(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(bg) }()
		if announce {
			if _, err := tx.Exec(bg, `SELECT set_config('scibox.audit_purge', 'on', true)`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.Exec(bg, `DELETE FROM audit_log WHERE id = $1`, id); err != nil {
			return err
		}
		return tx.Commit(bg)
	}
	isGuard := func(err error) bool {
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		return ok && strings.Contains(pgErr.Message, "append-only")
	}
	if err := del(true, young); !isGuard(err) {
		t.Errorf("a record younger than 90 days must not be deleted even in an announced purge: %v", err)
	}
	if err := del(false, older); !isGuard(err) {
		t.Errorf("without the announcement nothing is deleted: %v", err)
	}
	if err := del(true, older); err != nil {
		t.Errorf("an announced purge of an old record must work: %v", err)
	}
	for name, sql := range map[string]string{
		"update of the text":   fmt.Sprintf(`UPDATE audit_log SET target_label = 'x' WHERE id = %d`, young),
		"truncate":             `TRUNCATE audit_log`,
		"announced delete all": `DELETE FROM audit_log`,
	} {
		if _, err := testkit.Pool.Exec(bg, sql); !isGuard(err) {
			t.Errorf("%s must be refused: %v", name, err)
		}
	}
	if found, _ := auditExists(t, young); !found {
		t.Error("the young record vanished")
	}
}

func unconfirmed(t *testing.T, updated time.Time) (uuid.UUID, string) {
	t.Helper()
	var id uuid.UUID
	email := fmt.Sprintf("never%d@example.ru", testkit.Seq())
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO users (email, display_name, password_hash, privacy_consent_at, privacy_policy_version, created_at, updated_at)
		VALUES ($1, 'Не подтвердил', 'x', $2, 'v', $2, $2) RETURNING id`, email, updated).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, email
}

func TestUnconfirmedRegistrationsAreErased(t *testing.T) {
	now := clock()
	svc := service(t, Config{}, now)
	term := DefaultPolicy().UnconfirmedAccounts
	oldID, oldEmail := unconfirmed(t, now.Add(-term-time.Second))
	edgeID, _ := unconfirmed(t, now.Add(-term))
	freshID, _ := unconfirmed(t, now.Add(-day))
	liveLinkID, _ := unconfirmed(t, now.Add(-30*day))
	expiredLinkID, _ := unconfirmed(t, now.Add(-30*day))
	confirmed := newUser(t)
	exec(t, `UPDATE users SET updated_at = $2 WHERE id = $1`, confirmed, now.Add(-30*day))
	tombstone, _ := unconfirmed(t, now.Add(-30*day))
	exec(t, `UPDATE users SET deleted_at = $2 WHERE id = $1`, tombstone, now)
	link := func(user uuid.UUID, expires time.Time) {
		exec(t, `INSERT INTO auth_tokens (user_id, purpose, token_hash, created_at, expires_at) VALUES ($1, 'confirm_email', $2, $3, $4)`,
			user, []byte(uuid.NewString()), now.Add(-40*day), expires)
	}
	link(liveLinkID, now.Add(time.Hour))
	link(expiredLinkID, now.Add(-time.Hour))
	// Следы старой регистрации: письмо со ссылкой и счётчик попыток по её почте.
	exec(t, `INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at) VALUES ($1, 's', 'b', $2, $2)`, strings.ToUpper(oldEmail), now)
	exec(t, `INSERT INTO rate_events (kind, key, at) VALUES ('login_email', $1, $2)`, strings.ToLower(oldEmail), now)

	report, err := svc.Pass(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		id   uuid.UUID
		gone bool
	}{
		{"unconfirmed for longer than the term goes", oldID, true},
		{"unconfirmed exactly at the term stays", edgeID, false},
		{"recent registration stays", freshID, false},
		{"a live confirmation link keeps the registration", liveLinkID, false},
		{"an expired link does not", expiredLinkID, true},
		{"a confirmed account is never touched here", confirmed, false},
		{"a tombstone is not touched", tombstone, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := !exists(t, "users", c.id); got != c.gone {
				t.Errorf("removed = %v, want %v", got, c.gone)
			}
		})
	}
	if report.UnconfirmedAccounts != 2 {
		t.Errorf("report = %+v, want two registrations", report)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM outbox WHERE lower(to_email) = lower($1)`, oldEmail); n != 0 {
		t.Errorf("%d letters to the erased registration remain", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE key = lower($1)`, oldEmail); n != 0 {
		t.Errorf("%d attempt counters of the erased registration remain", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM auth_tokens WHERE user_id = $1`, expiredLinkID); n != 0 {
		t.Errorf("%d links of the erased registration remain", n)
	}
}

// Партии: Batch строк за транзакцию, MaxBatches партий за проход; остальное доберёт следующий проход.
func TestPassWorksInBatches(t *testing.T) {
	clean(t)
	now := clock()
	user := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	for range 5 {
		notification(t, user, old, &read)
	}
	count := func() int { return testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, user) }
	svc := service(t, Config{Batch: 2, MaxBatches: 2}, now)
	report, err := svc.Pass(bg)
	if err != nil || report.ReadNotifications != 4 || count() != 1 {
		t.Fatalf("first pass: %+v %v, left %d", report, err, count())
	}
	report, err = svc.Pass(bg)
	if err != nil || report.ReadNotifications != 1 || count() != 0 {
		t.Fatalf("second pass: %+v %v, left %d", report, err, count())
	}
}

// Другая копия сервера держит блокировку вида: этот вид пропускается, остальные работают.
func TestPassSkipsAKindThatAnotherCopyIsWorkingOn(t *testing.T) {
	clean(t)
	t.Cleanup(func() { clean(t) })
	now := clock()
	user := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	readID := notification(t, user, old, &read)
	unreadID := notification(t, user, old, nil)

	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	var got bool
	if err := tx.QueryRow(bg, `SELECT pg_try_advisory_xact_lock($1)`, tasks[0].lockKey).Scan(&got); err != nil || !got {
		t.Fatalf("lock: %v %v", got, err)
	}
	report, err := service(t, Config{}, now).Pass(bg)
	if err != nil {
		t.Fatal(err)
	}
	if report.ReadNotifications != 0 || !exists(t, "notifications", readID) {
		t.Error("the kind held by another copy must be skipped")
	}
	if exists(t, "notifications", unreadID) {
		t.Error("other kinds must still be cleaned")
	}
}

func TestTwoCopiesCleanTogetherWithoutDuplicatesOrErrors(t *testing.T) {
	clean(t)
	now := clock()
	user := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	const total = 60
	for range total {
		notification(t, user, old, &read)
	}
	var (
		wg      sync.WaitGroup
		deleted [4]int64
		errs    [4]error
	)
	for i := range deleted {
		wg.Go(func() {
			r, err := service(t, Config{Batch: 7}, now).Pass(bg)
			deleted[i], errs[i] = r.ReadNotifications, err
		})
	}
	wg.Wait()
	var sum int64
	for i := range deleted {
		if errs[i] != nil {
			t.Fatalf("copy %d: %v", i, errs[i])
		}
		sum += deleted[i]
	}
	if sum != total || testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, user) != 0 {
		t.Errorf("deleted %d of %d", sum, total)
	}
}

func TestPassDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		clean(t)
		now := clock()
		user := newUser(t)
		old := now.Add(-2 * 365 * day)
		read := old
		notification(t, user, old, &read)
		notification(t, user, old, nil)
		auditAt(t, user, now.AddDate(-4, 0, 0), "203.0.113.9")
		auditAt(t, user, now.Add(-200*day), "203.0.113.10")
		unconfirmed(t, now.Add(-30*day))
		return func(db testkit.DB) error {
			_, err := newService(db, Config{}, quiet(), func() time.Time { return now }).Pass(bg)
			return err
		}
	})
}

func TestPassKeepsGoingAfterOneKindFails(t *testing.T) {
	now := clock()
	user := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	readID := notification(t, user, old, &read)
	unreadID := notification(t, user, old, nil)
	// Ломается первое обращение к базе (начало первой транзакции): первый вид падает, второй работает.
	db, _ := testkit.Faulty(testkit.Pool, 1)
	report, err := newService(db, Config{}, quiet(), func() time.Time { return now }).Pass(bg)
	if !errors.Is(err, testkit.ErrFault) || !strings.Contains(err.Error(), "read notifications") {
		t.Fatalf("err = %v, want the failure of the first kind", err)
	}
	if !exists(t, "notifications", readID) || exists(t, "notifications", unreadID) || report.UnreadNotifications != 1 {
		t.Errorf("the failed kind must keep its rows and the others must run: %+v", report)
	}
}

func TestPassDoesNotLeaveAHalfDoneBatch(t *testing.T) {
	now := clock()
	user := newUser(t)
	actor := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	id := notification(t, user, old, &read)
	// Журнал: удаляемая запись и порог выше пола; сбой в середине партии откатывает её целиком.
	rec := auditAt(t, actor, now.AddDate(-4, 0, 0), "")
	for n := int32(1); n <= 12; n++ {
		db, calls := testkit.Faulty(testkit.Pool, n)
		_, err := newService(db, Config{}, quiet(), func() time.Time { return now }).Pass(bg)
		if calls.Load() < n {
			break
		}
		if err == nil {
			t.Fatalf("fault %d was swallowed", n)
		}
		if found, _ := auditExists(t, rec); !found && !errors.Is(err, testkit.ErrFault) {
			t.Fatalf("fault %d: record lost with a different error %v", n, err)
		}
	}
	// После всех сбоев чистка ещё может довести дело до конца.
	if _, err := service(t, Config{}, now).Pass(bg); err != nil {
		t.Fatal(err)
	}
	if exists(t, "notifications", id) {
		t.Error("the notification must be gone after a clean pass")
	}
	if found, _ := auditExists(t, rec); found {
		t.Error("the old record must be gone after a clean pass")
	}
}

func TestRunPassesAtStartAndStopsWithTheContext(t *testing.T) {
	now := clock()
	user := newUser(t)
	old := now.Add(-2 * 365 * day)
	read := old
	id := notification(t, user, old, &read)
	var logs bytes.Buffer
	var mu sync.Mutex
	svc := newService(testkit.Pool, Config{}, slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &mu}, nil)), func() time.Time { return now })
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { svc.Run(ctx, time.Hour); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for exists(t, "notifications", id) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop with the context")
	}
	if exists(t, "notifications", id) {
		t.Error("the first pass must run at once")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(logs.String(), "retention pass") || strings.Contains(logs.String(), "failed") {
		t.Errorf("log = %q", logs.String())
	}
}

func TestRunLogsAFailedPassButKeepsRunning(t *testing.T) {
	var logs bytes.Buffer
	var mu sync.Mutex
	db, _ := testkit.Faulty(testkit.Pool, 1)
	svc := newService(db, Config{}, slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &mu}, nil)), clock)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { svc.Run(ctx, time.Hour); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := strings.Contains(logs.String(), "retention pass failed")
		mu.Unlock()
		if got {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(logs.String(), "retention pass failed") {
		t.Errorf("a failed pass must be logged: %q", logs.String())
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
