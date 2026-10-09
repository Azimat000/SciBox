package applications

import (
	"errors"
	"slices"
	"testing"
	"time"

	"scibox/server/internal/audit"
	"scibox/server/internal/files"
	"scibox/server/internal/testaudit"
	"scibox/server/internal/testkit"
)

// applicationRows — записи журнала об откликах этой организации (руководителей team() назначил заранее).
func (w *world) applicationRows() []testaudit.Row {
	w.T.Helper()
	return testaudit.Rows(w.T, testkit.Pool, `org_id = $1 AND action LIKE 'application.%'`, w.tm.OrgID)
}

// Всё, что сотрудники делают с чужим откликом, остаётся в журнале: открыл, пригласил, решил, скачал файл.
func TestAuditTrailOfReviewing(t *testing.T) {
	w := newWorld(t)
	ctx := audit.WithSource(bg, "203.0.113.20")
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil, w.pdf("diploma.pdf"))

	// Соискатель сам читает и скачивает свой отклик: в журнал организации это не попадает.
	own, err := w.svc.Get(ctx, me.User, d.ID)
	if err != nil || own.CV == nil {
		t.Fatalf("own view: %+v, %v", own.CV, err)
	}
	if _, _, err := readFile(ctx, w.svc, me.User, d.ID, own.CV.ID); err != nil {
		t.Fatal(err)
	}
	if rows := w.applicationRows(); len(rows) != 0 {
		t.Fatalf("the applicant's own reading wrote %v", testaudit.Actions(rows))
	}

	// Сотрудник открывает карточку: первое открытие записывается, повторное нет.
	staff, err := w.svc.Get(ctx, w.tm.HeadA.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(ctx, w.tm.Owner.User, d.ID); err != nil {
		t.Fatal(err)
	}
	inv, err := w.svc.Invite(ctx, w.tm.HeadA.User, d.ID, w.interview())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerPropose, ProposedAt: w.when(100 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.AcceptProposal(ctx, w.tm.Owner.User, d.ID, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CancelInvitation(ctx, w.tm.Owner.User, d.ID, inv.ID); err != nil {
		t.Fatal(err)
	}
	// Скачивание: сотрудник записывается (и резюме, и приложенный файл), чужой руководитель получает «нет» без записи.
	if _, _, err := readFile(ctx, w.svc, w.tm.Owner.User, d.ID, staff.CV.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readFile(ctx, w.svc, w.tm.HR.User, d.ID, staff.Files[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readFile(ctx, w.svc, w.tm.HeadB.User, d.ID, staff.CV.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("head of another unit: %v", err)
	}
	if err := w.svc.Decide(ctx, w.tm.Owner.User, d.ID, StatusAccepted, "Рады видеть в команде"); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Decide(ctx, w.tm.Owner.User, d.ID, StatusRejected, ""); err == nil {
		t.Fatal("a decision cannot be reversed")
	}
	other := w.Applicant("Пётр")
	d2 := w.mustApply(other, nil)
	if _, err := w.svc.Get(bg, w.tm.HR.User, d2.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Decide(bg, w.tm.HR.User, d2.ID, StatusRejected, "Не подходит"); err != nil {
		t.Fatal(err)
	}

	rows := w.applicationRows()
	want := []string{
		"application.viewed", "application.invited", "application.proposal_accepted", "application.invitation_canceled",
		"application.file_downloaded", "application.file_downloaded", "application.accepted",
		"application.viewed", "application.rejected",
	}
	if got := testaudit.Actions(rows); !slices.Equal(got, want) {
		t.Fatalf("journal = %v\nwant      %v", got, want)
	}
	actors := []testkit.Person{w.tm.HeadA, w.tm.HeadA, w.tm.Owner, w.tm.Owner, w.tm.Owner, w.tm.HR, w.tm.Owner, w.tm.HR, w.tm.HR}
	for i, r := range rows {
		wantSubject := me
		if i >= 7 {
			wantSubject = other
		}
		wantID := d.ID
		if i >= 7 {
			wantID = d2.ID
		}
		if *r.Actor != actors[i].ID || *r.Subject != wantSubject.ID || r.TargetKind != "application" || *r.TargetID != wantID || r.TargetLabel != w.vacancy.Title {
			t.Errorf("entry %d (%s): %+v", i, r.Action, r)
		}
		if (i < 7) != (r.IP != nil && *r.IP == "203.0.113.20") {
			t.Errorf("entry %d ip = %v", i, r.IP)
		}
	}
	if rows[1].Details["kind"] != InvInterview || rows[4].Details["kind"] != string(files.KindCV) || rows[5].Details["kind"] != string(files.KindAttachment) {
		t.Errorf("details = %v / %v / %v", rows[1].Details, rows[4].Details, rows[5].Details)
	}
	// Ни текста письма, ни записки к решению, ни контактов в записях нет.
	for _, r := range rows {
		for _, v := range append([]string{r.TargetLabel}, mapValues(r.Details)...) {
			for _, secret := range []string{"Рады видеть в команде", "Не подходит", me.Email, "Мария"} {
				if v == secret {
					t.Errorf("%s keeps %q", r.Action, secret)
				}
			}
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// Отказ в правах и ошибка проверки ничего не записывают.
func TestRefusedReviewingLeavesNoTrace(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil, w.pdf("diploma.pdf"))
	staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID) // «просмотрен» записано здесь
	before := len(w.applicationRows())

	for name, do := range map[string]func() error{
		"outsider opens":       func() error { _, err := w.svc.Get(bg, w.tm.Out.User, d.ID); return err },
		"head of another unit": func() error { _, err := w.svc.Get(bg, w.tm.HeadB.User, d.ID); return err },
		"unknown application":  func() error { _, err := w.svc.Get(bg, w.tm.Owner.User, w.vacancy.ID); return err },
		"outsider decides":     func() error { return w.svc.Decide(bg, w.tm.Out.User, d.ID, StatusRejected, "") },
		"applicant decides":    func() error { return w.svc.Decide(bg, me.User, d.ID, StatusAccepted, "") },
		"wrong decision":       func() error { return w.svc.Decide(bg, w.tm.Owner.User, d.ID, "viewed", "") },
		"outsider invites":     func() error { _, err := w.svc.Invite(bg, w.tm.Out.User, d.ID, w.interview()); return err },
		"bad invitation": func() error {
			_, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, InvitationInput{Kind: InvInterview})
			return err
		},
		"outsider downloads":         func() error { _, _, err := readFile(bg, w.svc, w.tm.Out.User, d.ID, staff.CV.ID); return err },
		"unknown file":               func() error { _, _, err := readFile(bg, w.svc, w.tm.Owner.User, d.ID, d.ID); return err },
		"cancel an unknown invite":   func() error { return w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, d.ID) },
		"accept an unknown proposal": func() error { return w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, d.ID) },
	} {
		if err := do(); err == nil {
			t.Errorf("%s: not refused", name)
		}
	}
	if after := len(w.applicationRows()); after != before {
		t.Fatalf("refused actions wrote %d entries", after-before)
	}
}

// Нет записи — нет действия, а для файла нет записи — нет выдачи.
func TestReviewingFailsWhenTheJournalIsDown(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	fresh := w.mustApply(me, nil)
	seen := w.viewedApp(w.Applicant("Пётр"))
	olga := w.Applicant("Ольга")
	withFile := w.viewedApp(olga)
	staff, _ := w.svc.Get(bg, w.tm.Owner.User, withFile.ID)
	nina := w.Applicant("Нина") // подтверждение почты тоже входит в журнал: людей заводим до поломки
	testaudit.Break(t, testkit.Pool)

	// Открытие карточки в первый раз меняет статус, поэтому без записи не проходит.
	if _, err := w.svc.Get(bg, w.tm.Owner.User, fresh.ID); err == nil {
		t.Error("the card was opened without a journal entry")
	}
	if w.statusOf(fresh.ID) != StatusSent {
		t.Errorf("status = %s", w.statusOf(fresh.ID))
	}
	// Сам отклик читается соискателем без журнала.
	if _, err := w.svc.Get(bg, me.User, fresh.ID); err != nil {
		t.Errorf("the applicant must read his own application: %v", err)
	}
	if err := w.svc.Decide(bg, w.tm.Owner.User, seen.ID, StatusAccepted, ""); err == nil || w.statusOf(seen.ID) != StatusViewed {
		t.Errorf("decision: %v, status %s", err, w.statusOf(seen.ID))
	}
	if _, err := w.svc.Invite(bg, w.tm.Owner.User, seen.ID, w.interview()); err == nil || w.statusOf(seen.ID) != StatusViewed {
		t.Errorf("invitation: %v, status %s", err, w.statusOf(seen.ID))
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_invitations WHERE application_id = $1`, seen.ID); n != 0 {
		t.Errorf("%d invitations were created", n)
	}
	name, data, err := readFile(bg, w.svc, w.tm.Owner.User, withFile.ID, staff.CV.ID)
	if err == nil || name != "" || data != nil {
		t.Errorf("the file was handed out without a journal entry: %q %d bytes, %v", name, len(data), err)
	}
	if _, _, err := readFile(bg, w.svc, nina.User, withFile.ID, staff.CV.ID); err == nil {
		t.Error("a stranger got the file")
	}
	// Свой файл соискатель получает и при неисправном журнале.
	own, _ := w.svc.Get(bg, olga.User, withFile.ID)
	if _, _, err := readFile(bg, w.svc, olga.User, withFile.ID, own.CV.ID); err != nil {
		t.Errorf("own file: %v", err)
	}
}
