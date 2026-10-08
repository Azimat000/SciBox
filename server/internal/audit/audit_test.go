package audit_test

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.RunMain(m)) }

var bg = testkit.BG

// record пишет события одной транзакцией и подтверждает её.
func record(t *testing.T, ctx context.Context, events ...audit.Event) {
	t.Helper()
	tx, err := testkit.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	for _, e := range events {
		if err := audit.Record(ctx, q, e); err != nil {
			t.Fatalf("record %s: %v", e.Action, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func list(t *testing.T, org uuid.UUID, before int64, limit int) audit.Page {
	t.Helper()
	p, err := audit.List(bg, dbgen.New(testkit.Pool), org, before, limit)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestActionsAreWellFormed(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)
	all := audit.Actions()
	if len(all) < 30 {
		t.Fatalf("only %d actions are registered", len(all))
	}
	seen := map[audit.Action]bool{}
	for _, a := range all {
		if !shape.MatchString(string(a)) || len(a) > 60 {
			t.Errorf("action %q does not fit the table check", a)
		}
		if seen[a] {
			t.Errorf("action %q is registered twice", a)
		}
		seen[a] = true
	}
	for a, wantOrg := range map[audit.Action]bool{
		audit.MemberRoleChanged: true, audit.VacancyPublished: true, audit.ApplicationFileDownloaded: true, audit.OfferSent: true,
		audit.AccountLogin: false, audit.AccountEmailChanged: false, audit.AccountErased: false, "no.such": false,
	} {
		if got := audit.OrgScoped(a); got != wantOrg {
			t.Errorf("OrgScoped(%s) = %v", a, got)
		}
	}
}

func TestRecordAndList(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	other := w.Team()
	target := uuid.New()
	record(t, testkit.BG,
		audit.Event{Action: audit.MemberRoleChanged, OrgID: tm.OrgID, Actor: tm.Owner.ID, Subject: tm.HR.ID,
			Details: map[string]string{"from": "hr", "to": "unit_head"}},
		audit.Event{Action: audit.VacancyPublished, OrgID: tm.OrgID, Actor: tm.HeadA.ID, TargetKind: "vacancy", TargetID: target,
			TargetLabel: "Старший научный сотрудник", Details: map[string]string{"from": "draft", "to": "published"}},
		audit.Event{Action: audit.AccountLogin, Actor: tm.Owner.ID}, // личный журнал: в журнал организации не попадает
		audit.Event{Action: audit.OfferSent, OrgID: other.OrgID, Actor: other.Owner.ID},
	)

	got := list(t, tm.OrgID, 0, 0)
	if len(got.Items) != 2 || got.Next != nil {
		t.Fatalf("items = %d, next = %v; want the two events of this organization only", len(got.Items), got.Next)
	}
	// Новые записи первыми.
	pub, role := got.Items[0], got.Items[1]
	if pub.Action != audit.VacancyPublished || role.Action != audit.MemberRoleChanged {
		t.Fatalf("order = %s, %s", pub.Action, role.Action)
	}
	if pub.Actor == nil || pub.Actor.ID != tm.HeadA.ID || pub.Actor.Name != "Руководитель А" || pub.Subject != nil {
		t.Errorf("publish actor/subject = %+v / %+v", pub.Actor, pub.Subject)
	}
	if pub.Target.Kind != "vacancy" || pub.Target.ID == nil || *pub.Target.ID != target || pub.Target.Label != "Старший научный сотрудник" {
		t.Errorf("target = %+v", pub.Target)
	}
	if pub.Details["to"] != "published" || role.Details["from"] != "hr" {
		t.Errorf("details = %v / %v", pub.Details, role.Details)
	}
	if role.Subject == nil || role.Subject.Name != "Кадровик" || role.Target.ID != nil {
		t.Errorf("role subject/target = %+v / %+v", role.Subject, role.Target)
	}
	if pub.At.IsZero() || pub.ID <= role.ID {
		t.Errorf("at = %v, ids %d %d", pub.At, pub.ID, role.ID)
	}

	if theirs := list(t, other.OrgID, 0, 10); len(theirs.Items) != 1 || theirs.Items[0].Action != audit.OfferSent {
		t.Errorf("other organization sees %+v", theirs.Items)
	}
	if none := list(t, uuid.New(), 0, 10); len(none.Items) != 0 || none.Items == nil {
		t.Errorf("unknown organization: %+v (the list must be empty, not null)", none.Items)
	}
}

func TestListPages(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	for i := range 7 {
		record(t, bg, audit.Event{Action: audit.VacancyClosed, OrgID: tm.OrgID, Actor: tm.Owner.ID, TargetLabel: string(rune('A' + i))})
	}
	// Страницы по три: 3 + 3 + 1, каждая запись ровно один раз.
	var labels []string
	var before int64
	for page := 1; page <= 3; page++ {
		p := list(t, tm.OrgID, before, 3)
		want := 3
		if page == 3 {
			want = 1
		}
		if len(p.Items) != want {
			t.Fatalf("page %d has %d items", page, len(p.Items))
		}
		for _, e := range p.Items {
			labels = append(labels, e.Target.Label)
		}
		if (p.Next == nil) != (page == 3) {
			t.Fatalf("page %d next = %v", page, p.Next)
		}
		if p.Next != nil {
			before = *p.Next
		}
	}
	if strings.Join(labels, "") != "GFEDCBA" {
		t.Fatalf("labels = %v", labels)
	}
	// Ровно на границе страницы продолжения нет.
	if p := list(t, tm.OrgID, 0, 7); len(p.Items) != 7 || p.Next != nil {
		t.Errorf("exact page: %d items, next %v", len(p.Items), p.Next)
	}
	// Предел страницы и умолчание.
	if p := list(t, tm.OrgID, 0, 100000); len(p.Items) != 7 {
		t.Errorf("huge limit gave %d", len(p.Items))
	}
	if p := list(t, tm.OrgID, -5, -1); len(p.Items) != 7 {
		t.Errorf("negative cursor and limit gave %d", len(p.Items))
	}
	if p := list(t, tm.OrgID, 1, 10); len(p.Items) != 0 {
		t.Errorf("before the very first entry: %d items", len(p.Items))
	}
}

func TestRecordRollsBackWithTheAction(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(bg, dbgen.New(tx), audit.Event{Action: audit.MemberLeft, OrgID: tm.OrgID, Actor: tm.HR.ID}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(bg); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, tm.OrgID); n != 0 {
		t.Fatalf("%d entries survived a rolled back transaction", n)
	}
}

func TestRecordRejectsMistakes(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	q := dbgen.New(testkit.Pool)
	tests := []struct {
		name string
		e    audit.Event
		want error
	}{
		{"unknown action", audit.Event{Action: "vacancy.exploded", OrgID: tm.OrgID}, audit.ErrUnknownAction},
		{"empty action", audit.Event{OrgID: tm.OrgID}, audit.ErrUnknownAction},
		{"organization action without organization", audit.Event{Action: audit.MemberLeft, Actor: tm.HR.ID}, audit.ErrScope},
		{"account action with organization", audit.Event{Action: audit.AccountLogin, OrgID: tm.OrgID, Actor: tm.HR.ID}, audit.ErrScope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := audit.Record(bg, q, tc.e); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	// Ссылки проверяет база: записи об организации, которой нет, не бывает.
	err := audit.Record(bg, q, audit.Event{Action: audit.MemberLeft, OrgID: uuid.New()})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23503" {
		t.Errorf("unknown organization: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, tm.OrgID); n != 0 {
		t.Errorf("%d entries were written by rejected events", n)
	}
}

func TestRecordKeepsEverythingWithinTheTableLimits(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	huge := strings.Repeat("Ж", 5000)
	details := map[string]string{"": "empty key is dropped", "ключ": huge, "html": "<b>&</b>", "control": "a\x00b\nc\td", "bad": "\xff\xfe"}
	for i := range 20 {
		details["z"+string(rune('a'+i))] = huge
	}
	record(t, bg, audit.Event{
		Action: audit.VacancyArchived, OrgID: tm.OrgID, Actor: tm.Owner.ID, TargetKind: strings.Repeat("k", 90),
		TargetLabel: strings.Repeat("Я", 1000) + "\n\x00", Details: details, IP: strings.Repeat("1", 200),
	})
	e := list(t, tm.OrgID, 0, 1).Items[0]
	if n := len([]rune(e.Target.Label)); n != 300 || !strings.HasSuffix(e.Target.Label, "…") {
		t.Errorf("label has %d runes: %q", n, e.Target.Label[:10])
	}
	if len([]rune(e.Target.Kind)) != 30 {
		t.Errorf("kind has %d runes", len([]rune(e.Target.Kind)))
	}
	if len(e.Details) != 6 {
		t.Errorf("details has %d pairs, want 6: %v", len(e.Details), e.Details)
	}
	if _, ok := e.Details[""]; ok {
		t.Error("an empty key survived")
	}
	for k, v := range e.Details {
		if len([]rune(k)) > 20 || len([]rune(v)) > 60 {
			t.Errorf("detail %q has %d runes", k, len([]rune(v)))
		}
		if strings.ContainsAny(v, "\x00\n\t") {
			t.Errorf("detail %q keeps control characters", k)
		}
	}
	if e.Details["html"] != "<b>&</b>" || e.Details["control"] != "a b c d" || e.Details["bad"] != "�" {
		t.Errorf("details = %v", e.Details)
	}
	var ip string
	if err := testkit.Pool.QueryRow(bg, `SELECT ip FROM audit_log WHERE id = $1`, e.ID).Scan(&ip); err != nil || len([]rune(ip)) != 45 {
		t.Errorf("ip = %q, %v", ip, err)
	}
}

func TestRecordUsesTheAddressOfTheRequest(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	ctx := audit.WithSource(bg, "203.0.113.7")
	record(t, ctx, audit.Event{Action: audit.MemberLeft, OrgID: tm.OrgID, Actor: tm.HR.ID})
	record(t, ctx, audit.Event{Action: audit.MemberRemoved, OrgID: tm.OrgID, Actor: tm.Owner.ID, IP: "198.51.100.9"})
	record(t, bg, audit.Event{Action: audit.MemberJoined, OrgID: tm.OrgID, Actor: tm.HeadA.ID})
	rows, err := testkit.Pool.Query(bg, `SELECT action, ip FROM audit_log WHERE org_id = $1 ORDER BY id`, tm.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]*string{}
	for rows.Next() {
		var action string
		var ip *string
		if err := rows.Scan(&action, &ip); err != nil {
			t.Fatal(err)
		}
		got[action] = ip
	}
	if *got["member.left"] != "203.0.113.7" || *got["member.removed"] != "198.51.100.9" || got["member.joined"] != nil {
		t.Errorf("addresses = %v %v %v", *got["member.left"], *got["member.removed"], got["member.joined"])
	}
	if audit.SourceIP(bg) != "" || audit.SourceIP(ctx) != "203.0.113.7" {
		t.Error("SourceIP does not return what WithSource stored")
	}
}

func TestRecordUsesTheGivenTime(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	record(t, bg, audit.Event{Action: audit.MemberLeft, OrgID: tm.OrgID, Actor: tm.HR.ID, At: at})
	record(t, bg, audit.Event{Action: audit.MemberJoined, OrgID: tm.OrgID, Actor: tm.HR.ID})
	items := list(t, tm.OrgID, 0, 10).Items
	if !items[1].At.Equal(at) || time.Since(items[0].At) > time.Minute {
		t.Errorf("times = %v, %v", items[1].At, items[0].At)
	}
}

// Запись нельзя ни изменить, ни удалить; единственное разрешённое изменение — убрать адрес.
func TestTheLogIsAppendOnly(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	record(t, audit.WithSource(bg, "203.0.113.7"), audit.Event{Action: audit.MemberRemoved, OrgID: tm.OrgID, Actor: tm.Owner.ID, Subject: tm.HR.ID,
		TargetLabel: "x", Details: map[string]string{"a": "b"}})
	id := list(t, tm.OrgID, 0, 1).Items[0].ID

	denied := []struct{ name, sql string }{
		{"delete", `DELETE FROM audit_log WHERE id = $1`},
		{"truncate", `TRUNCATE audit_log`},
		{"change the action", `UPDATE audit_log SET action = 'member.left' WHERE id = $1`},
		{"change the time", `UPDATE audit_log SET at = at + interval '1 day' WHERE id = $1`},
		{"change the actor", `UPDATE audit_log SET actor_id = NULL WHERE id = $1`},
		{"change the subject", `UPDATE audit_log SET subject_id = NULL WHERE id = $1`},
		{"change the organization", `UPDATE audit_log SET org_id = NULL WHERE id = $1`},
		{"change the label", `UPDATE audit_log SET target_label = 'y' WHERE id = $1`},
		{"change the details", `UPDATE audit_log SET details = '{}' WHERE id = $1`},
		{"change the address", `UPDATE audit_log SET ip = '10.0.0.1' WHERE id = $1`},
		{"clear the address and something else", `UPDATE audit_log SET ip = NULL, target_label = 'y' WHERE id = $1`},
	}
	for _, tc := range denied {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if strings.HasPrefix(tc.sql, "TRUNCATE") {
				_, err = testkit.Pool.Exec(bg, tc.sql)
			} else {
				_, err = testkit.Pool.Exec(bg, tc.sql, id)
			}
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23000" {
				t.Fatalf("err = %v, want integrity_constraint_violation", err)
			}
		})
	}
	var action, label string
	var ip *string
	if err := testkit.Pool.QueryRow(bg, `SELECT action, target_label, ip FROM audit_log WHERE id = $1`, id).Scan(&action, &label, &ip); err != nil {
		t.Fatalf("the entry is gone or changed: %v", err)
	}
	if action != "member.removed" || label != "x" || ip == nil {
		t.Fatalf("entry = %s %s %v", action, label, ip)
	}

	// Адрес стирается запросом, который вызывает стирание аккаунта.
	n, err := dbgen.New(testkit.Pool).ScrubAuditIP(bg, &tm.Owner.ID)
	if err != nil || n != 1 {
		t.Fatalf("scrub: %d, %v", n, err)
	}
	e := list(t, tm.OrgID, 0, 1).Items[0]
	if e.Action != audit.MemberRemoved || e.Target.Label != "x" || e.Details["a"] != "b" || e.Actor == nil || e.Subject == nil {
		t.Errorf("scrubbing changed more than the address: %+v", e)
	}
	if err := testkit.Pool.QueryRow(bg, `SELECT ip FROM audit_log WHERE id = $1`, id).Scan(&ip); err != nil || ip != nil {
		t.Errorf("address after scrub = %v, %v", ip, err)
	}
	if n, err := dbgen.New(testkit.Pool).ScrubAuditIP(bg, &tm.Owner.ID); err != nil || n != 0 {
		t.Errorf("second scrub: %d, %v", n, err)
	}
}

func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	w := testkit.NewWorld(t)
	tm := w.Team()
	record(t, bg, audit.Event{Action: audit.MemberLeft, OrgID: tm.OrgID, Actor: tm.HR.ID, Details: map[string]string{"a": "b"}})
	t.Run("record", func(t *testing.T) {
		testkit.RunFaults(t, func(*testing.T) func(db testkit.DB) error {
			return func(db testkit.DB) error {
				return audit.Record(bg, dbgen.New(db), audit.Event{Action: audit.MemberLeft, OrgID: tm.OrgID, Actor: tm.HR.ID})
			}
		})
	})
	t.Run("list", func(t *testing.T) {
		testkit.RunFaults(t, func(*testing.T) func(db testkit.DB) error {
			return func(db testkit.DB) error {
				_, err := audit.List(bg, dbgen.New(db), tm.OrgID, 0, 10)
				return err
			}
		})
	})
	t.Run("a damaged entry is an error, not a silent gap", func(t *testing.T) {
		// Таблица не даёт записать не объект, поэтому подставляем ответ базы.
		_, err := audit.List(bg, dbgen.New(brokenDetails{testkit.Pool}), tm.OrgID, 0, 10)
		if err == nil || !strings.Contains(err.Error(), "decode details") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestMaskEmail(t *testing.T) {
	tests := []struct{ in, want string }{
		{"anna.petrova@msu.ru", "a***@msu.ru"},
		{"  Anna@MSU.ru ", "A***@msu.ru"},
		{"Яна@почта.рф", "Я***@почта.рф"},
		{"a@b.c", "a***@b.c"},
		{"no-at-sign", "***"},
		{"@msu.ru", "***"},
		{"anna@", "***"},
		{"", "***"},
		{"two@at@signs.ru", "t***@at@signs.ru"},
		{"long.address@Example.R", "l***@example.r"},
	}
	for _, tc := range tests {
		if got := audit.MaskEmail(tc.in); got != tc.want {
			t.Errorf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// brokenDetails подменяет ответ базы: подробности не объект.
type brokenDetails struct{ testkit.DB }

func (b brokenDetails) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return b.DB.Query(ctx, strings.Replace(sql, "a.details", "'[1]'::jsonb AS details", 1), args...)
}
