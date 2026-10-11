package auth

import (
	"strings"
	"testing"
	"time"
)

// setInactive делает аккаунт «не входившим» с момента ago назад по часам теста.
func (e *env) setInactive(email string, ago time.Duration) {
	e.t.Helper()
	if _, err := sharedPool.Exec(bg, `UPDATE users SET last_active_at = $2, inactive_warned_at = NULL WHERE email = $1`, email, e.clock.Now().Add(-ago)); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) isErased(email string) bool {
	e.t.Helper()
	return countRows(e.t, `SELECT count(*) FROM users WHERE email = $1`, email) == 0
}

func (e *env) warnings(email string) int {
	n := 0
	for _, m := range e.mailsTo(email) {
		if strings.Contains(m.Subject, "будет удалён") && strings.Contains(m.Body, "давно не входили") {
			n++
		}
	}
	return n
}

// Заброшенный аккаунт (D-192): письмо за 30 дней, потом стирание; вход в срок всё отменяет; свежие и неподтверждённые
// не трогаются.
func TestInactiveAccountsAreWarnedThenErased(t *testing.T) {
	e := newEnv(t)
	_, _, abandoned := e.confirmedUser()
	_, _, soon := e.confirmedUser()
	_, _, active := e.confirmedUser()
	unconfirmed := uniqueEmail()
	e.register(unconfirmed)

	year := 365 * 24 * time.Hour
	e.setInactive(abandoned, 3*year-10*24*time.Hour) // до предела ещё 10 дней: уже пора предупредить
	e.setInactive(soon, 2*year)                      // ещё далеко
	e.setInactive(active, 3*year+time.Hour)          // за пределом, но человек сейчас войдёт
	e.setInactive(unconfirmed, 10*year)              // неподтверждённых чистит другая уборка

	warned, scheduled, err := e.svc.ProcessInactive(bg)
	if err != nil {
		t.Fatal(err)
	}
	// Предупреждены оба: тот, кому осталось 10 дней, и тот, кто за пределом (его предупреждение — отсчёт 30 дней).
	if warned != 2 || scheduled != 0 {
		t.Fatalf("warned %d, scheduled %d; want 2, 0", warned, scheduled)
	}
	if e.warnings(abandoned) != 1 || e.warnings(active) != 1 || e.warnings(soon) != 0 || e.warnings(unconfirmed) != 0 {
		t.Fatalf("warnings: abandoned %d, active %d, soon %d, unconfirmed %d", e.warnings(abandoned), e.warnings(active), e.warnings(soon), e.warnings(unconfirmed))
	}
	// Повторный проход предупреждение не дублирует.
	if warned, _, _ := e.svc.ProcessInactive(bg); warned != 0 {
		t.Fatalf("a second pass warned %d", warned)
	}

	// Один из предупреждённых входит и остаётся.
	if _, _, err := e.login(active, goodPassword); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(31 * 24 * time.Hour)
	if _, err := sharedPool.Exec(bg, `UPDATE users SET last_active_at = $2 WHERE email = $1`, active, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, scheduled, err = e.svc.ProcessInactive(bg); err != nil || scheduled != 1 {
		t.Fatalf("scheduled %d, %v; want 1", scheduled, err)
	}
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	if !e.isErased(abandoned) {
		t.Fatal("the abandoned account was not erased")
	}
	if e.isErased(active) || e.isErased(soon) || e.isErased(unconfirmed) {
		t.Fatal("an account that must stay was erased")
	}
}

// Любой вход начинает счёт заново и снимает предупреждение.
func TestSigningInRestartsTheInactivityCount(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	e.setInactive(email, 3*365*24*time.Hour+24*time.Hour)
	if warned, _, _ := e.svc.ProcessInactive(bg); warned != 1 {
		t.Fatalf("warned %d", warned)
	}
	e.clock.Advance(24 * time.Hour)
	if _, _, err := e.login(email, goodPassword); err != nil {
		t.Fatal(err)
	}
	if countRows(t, `SELECT count(*) FROM users WHERE email = $1 AND inactive_warned_at IS NULL AND last_active_at >= $2`, email, e.clock.Now().Add(-time.Minute)) != 1 {
		t.Fatal("a login must restart the count and clear the warning")
	}
	e.clock.Advance(40 * 24 * time.Hour)
	if _, scheduled, err := e.svc.ProcessInactive(bg); err != nil || scheduled != 0 {
		t.Fatalf("scheduled %d, %v after a login", scheduled, err)
	}
}

// Ноль в настройках не значит «удалять сразу».
func TestInactivityConfigNeverMeansImmediately(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	e.setInactive(email, 24*time.Hour)
	e.svc.cfg.InactiveAfter, e.svc.cfg.InactiveWarn = 0, 0
	warned, scheduled, err := e.svc.ProcessInactive(bg)
	if err != nil || warned != 0 || scheduled != 0 {
		t.Fatalf("warned %d, scheduled %d, %v", warned, scheduled, err)
	}
}
