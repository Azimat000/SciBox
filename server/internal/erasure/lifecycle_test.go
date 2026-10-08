package erasure_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/orgs"
	"scibox/server/internal/testkit"
)

// Весь путь удаления через сервис аккаунтов (T-11, D-142): просьба → срок отмены → отмена → снова → стирание.
func TestDeletionLifecycle(t *testing.T) {
	w := newWorld(t)
	who := w.Applicant("Передумавший Соискатель")
	if _, err := w.Prof.SetPrivacy(bg, who.User, "orgs", false); err != nil {
		t.Fatal(err)
	}
	p := fresh(t, w.World, who.Email)

	start := time.Now().UTC()
	due, err := w.Auth.RequestDeletion(bg, p)
	if err != nil {
		t.Fatal(err)
	}
	if d := due.Sub(start); d < auth.DefaultDeletionGrace-time.Minute || d > auth.DefaultDeletionGrace+time.Minute {
		t.Fatalf("due in %v, want %v", d, auth.DefaultDeletionGrace)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, who.ID); n != 0 {
		t.Fatal("asking for deletion ends every session")
	}
	if vis := visibility(t, who.ID); vis != "hidden/orgs" {
		t.Fatalf("profile during the grace period: %s", vis)
	}
	if !mailed(w.World, who.Email, "будет удалён") {
		t.Fatal("the person must get a mail with the date")
	}
	// Повторная просьба срок не сдвигает.
	p2 := fresh(t, w.World, who.Email)
	if p2.User.DeletionDueAt == nil || !p2.User.DeletionDueAt.Equal(due) {
		t.Fatalf("login during the grace period must work and show the date: %+v", p2.User)
	}
	if again, err := w.Auth.RequestDeletion(bg, p2); err != nil || !again.Equal(due) {
		t.Fatalf("second request: %v %v", again, err)
	}

	p3 := fresh(t, w.World, who.Email)
	if err := w.Auth.CancelDeletion(bg, p3); err != nil {
		t.Fatal(err)
	}
	if vis := visibility(t, who.ID); vis != "orgs/" {
		t.Fatalf("profile after cancelling: %s", vis)
	}
	if !mailed(w.World, who.Email, "отменено") {
		t.Fatal("cancelling must be confirmed by mail")
	}
	// Отмена без назначенного удаления ничего не делает и писем не шлёт.
	before := len(w.Mail.Sent())
	if err := w.Auth.CancelDeletion(bg, p3); err != nil {
		t.Fatal(err)
	}
	w.Auth.Flush()
	if len(w.Mail.Sent()) != before {
		t.Fatal("nothing to cancel, nothing to mail")
	}

	// Снова просит, срок прошёл — стирание.
	if _, err := w.Auth.RequestDeletion(bg, fresh(t, w.World, who.Email)); err != nil {
		t.Fatal(err)
	}
	// Раньше срока фоновая задача его не трогает.
	if n, err := w.Auth.ProcessDueDeletions(bg); err != nil || n != 0 {
		t.Fatalf("before the due date: %d %v", n, err)
	}
	exec(t, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id = $1`, who.ID)
	if n, err := w.Auth.ProcessDueDeletions(bg); err != nil || n != 1 {
		t.Fatalf("due deletion: %d %v", n, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL AND deletion_due_at IS NULL`, who.ID); n != 1 {
		t.Fatal("the account must be erased")
	}
	if !mailed(w.World, who.Email, "удалён") {
		t.Fatal("the last mail goes to the old address")
	}
	// Стёртый аккаунт не возвращается: ни входом, ни сбросом, ни регистрацией с адресом-заглушкой.
	if _, _, err := w.Auth.Login(bg, who.Email, testkit.Password, testkit.Meta()); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login after erasure: %v", err)
	}
	tomb := "deleted+" + who.ID.String() + "@deleted"
	if _, _, err := w.Auth.Login(bg, tomb, "!", testkit.Meta()); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login with the tombstone address: %v", err)
	}
	before = len(w.Mail.Sent())
	if err := w.Auth.RequestPasswordReset(bg, tomb, testkit.Meta()); err != nil {
		t.Fatal(err)
	}
	w.Auth.Flush()
	if len(w.Mail.Sent()) != before {
		t.Fatal("no reset for an erased account")
	}
	if _, err := w.Auth.Register(bg, auth.RegisterInput{Name: "Захватчик", Email: tomb, Password: testkit.Password, Consent: true}, testkit.Meta()); err == nil {
		t.Fatal("the tombstone address cannot be registered")
	}
	// Прежнюю почту можно занять заново: это новый человек.
	if _, err := w.Auth.Register(bg, auth.RegisterInput{Name: "Новый", Email: who.Email, Password: testkit.Password, Consent: true}, testkit.Meta()); err != nil {
		t.Fatalf("the old address is free again: %v", err)
	}
}

func TestRequestDeletionNeedsFreshPassword(t *testing.T) {
	w := newWorld(t)
	who := w.User("Без пароля")
	p, err := w.Auth.Authenticate(bg, who.Token) // сессия по ссылке из письма
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Auth.RequestDeletion(bg, p); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("without a fresh password: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM users WHERE id = $1 AND deletion_due_at IS NULL`, who.ID); n != 1 {
		t.Fatal("nothing must be scheduled")
	}
}

func TestRequestDeletionBlockedForSoleOwnerWithStaff(t *testing.T) {
	w := newWorld(t)
	p := fresh(t, w.World, w.tm.Owner.Email)
	plan, err := w.Auth.DeletionPlan(bg, p)
	if err != nil || !plan.Blocked() || plan.Blocking[0].Slug != w.tm.Slug {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	_, err = w.Auth.RequestDeletion(bg, p)
	if _, ok := errors.AsType[*erasure.BlockedError](err); !ok {
		t.Fatalf("RequestDeletion = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, w.tm.Owner.ID); n == 0 {
		t.Fatal("a refused request must not end sessions")
	}
	// Сделал кадровика владельцем — теперь можно.
	if err := w.Orgs.ChangeRole(bg, p, w.tm.Slug, w.tm.HR.ID, string(access.RoleOwner)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Auth.RequestDeletion(bg, p); err != nil {
		t.Fatalf("with a co-owner: %v", err)
	}
}

// За срок отмены в организации одиночки появился сотрудник: стирать нельзя, удаление отменяется с письмом.
func TestDueDeletionIsCancelledWhenStaffAppeared(t *testing.T) {
	w := newWorld(t)
	lone := w.Applicant("Владелец-одиночка")
	org, err := w.Orgs.CreateOrganization(bg, lone.User, orgs.OrgInput{Name: fmt.Sprintf("Кафедра одного %d", testkit.Seq()), Kind: orgs.KindUniversity, City: "Казань"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Prof.SetPrivacy(bg, lone.User, "public", false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Auth.RequestDeletion(bg, fresh(t, w.World, lone.Email)); err != nil {
		t.Fatal(err)
	}
	newcomer := w.User("Новый сотрудник")
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: org.ID, UserID: newcomer.ID, Role: "hr", JoinedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id = $1`, lone.ID)
	if n, err := w.Auth.ProcessDueDeletions(bg); err != nil || n != 0 {
		t.Fatalf("blocked deletion: %d %v", n, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NULL AND deletion_due_at IS NULL`, lone.ID); n != 1 {
		t.Fatal("the deletion must be cancelled, not retried forever")
	}
	if vis := visibility(t, lone.ID); vis != "public/" {
		t.Fatalf("profile must be visible again: %s", vis)
	}
	var found bool
	for _, m := range w.Mail.Sent() {
		if m.To == lone.Email && strings.Contains(m.Subject, "Не удалось") && strings.Contains(m.Body, org.Name) {
			found = true
		}
	}
	if !found {
		t.Fatal("the person must learn why and what to do")
	}
}

// Две копии сервера одновременно: каждый аккаунт стирается один раз.
func TestConcurrentDueDeletions(t *testing.T) {
	w := newWorld(t)
	var ids []uuid.UUID
	for i := range 4 {
		p := w.Applicant(fmt.Sprintf("Уходящий %d", i))
		if _, err := w.Auth.RequestDeletion(bg, fresh(t, w.World, p.Email)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	exec(t, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id = ANY($1)`, ids)
	other := testkit.NewWorld(t)
	results := make(chan int, 2)
	errs := make(chan error, 2)
	for _, svc := range []*auth.Service{w.Auth, other.Auth} {
		go func() {
			n, err := svc.ProcessDueDeletions(bg)
			results <- n
			errs <- err
		}()
	}
	total := 0
	for range 2 {
		total += <-results
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if total != len(ids) {
		t.Fatalf("erased %d accounts, want %d", total, len(ids))
	}
	if n := testkit.Count(t, `SELECT count(*) FROM users WHERE id = ANY($1) AND deleted_at IS NOT NULL`, ids); n != len(ids) {
		t.Fatalf("erased in the database: %d", n)
	}
}

// Сбой базы при просьбе, отмене и стирании доходит до вызывающего (I-09).
func TestDeletionDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	w := newWorld(t)
	acfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	acfg.Hash = auth.TestHashParams
	t.Run("request", func(t *testing.T) {
		testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
			p := fresh(t, w.World, w.Applicant("Сбой просьбы").Email)
			return func(db testkit.DB) error {
				_, err := auth.NewServiceOn(db, w.Mail, acfg, w.Log).RequestDeletion(bg, p)
				return err
			}
		})
	})
	t.Run("cancel", func(t *testing.T) {
		testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
			who := w.Applicant("Сбой отмены")
			if _, err := w.Auth.RequestDeletion(bg, fresh(t, w.World, who.Email)); err != nil {
				t.Fatal(err)
			}
			p := fresh(t, w.World, who.Email)
			return func(db testkit.DB) error { return auth.NewServiceOn(db, w.Mail, acfg, w.Log).CancelDeletion(bg, p) }
		})
	})
	t.Run("process", func(t *testing.T) {
		testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
			exec(t, `UPDATE users SET deletion_due_at = NULL WHERE deletion_due_at IS NOT NULL AND deleted_at IS NULL`)
			who := w.Applicant("Сбой стирания")
			if _, err := w.Auth.RequestDeletion(bg, fresh(t, w.World, who.Email)); err != nil {
				t.Fatal(err)
			}
			exec(t, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id = $1`, who.ID)
			return func(db testkit.DB) error {
				_, err := auth.NewServiceOn(db, w.Mail, acfg, w.Log).ProcessDueDeletions(bg)
				return err
			}
		})
	})
}

func visibility(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	var vis string
	var before *string
	if err := testkit.Pool.QueryRow(bg, `SELECT visibility, visibility_before_deletion FROM profiles WHERE user_id = $1`, userID).Scan(&vis, &before); err != nil {
		t.Fatal(err)
	}
	if before == nil {
		return vis + "/"
	}
	return vis + "/" + *before
}

func mailed(w *testkit.World, to, subject string) bool {
	w.Auth.Flush()
	for _, m := range w.Mail.Sent() {
		if m.To == to && strings.Contains(m.Subject, subject) {
			return true
		}
	}
	return false
}
