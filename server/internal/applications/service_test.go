package applications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/profiles"
	"scibox/server/internal/references"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

func TestApplyHappyPath(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	ref1, ref2 := refereeIn(), refereeIn()
	d, err := w.apply(me, func(in *Input) {
		in.CoverLetter = "  Здравствуйте! Мой опыт работы с катализаторами подходит для вашей лаборатории.  "
		in.ContactEmail = "  Maria.Apply@Example.ru "
		in.Referees = []references.RefereeInput{ref1, ref2}
	}, pdf(`C:\Мои документы\Список публикаций.pdf`), pdf("diploma"))
	if err != nil {
		t.Fatal(err)
	}

	if d.Status != StatusSent || d.Viewer.Role != RoleApplicant || !d.Viewer.CanWithdraw {
		t.Errorf("status/viewer = %s %+v", d.Status, d.Viewer)
	}
	if d.ApplicantName != "Мария Смирнова" || d.ContactEmail != "maria.apply@example.ru" {
		t.Errorf("applicant = %q contact = %q", d.ApplicantName, d.ContactEmail)
	}
	if d.CoverLetter != "Здравствуйте! Мой опыт работы с катализаторами подходит для вашей лаборатории." {
		t.Errorf("cover letter = %q", d.CoverLetter)
	}
	if d.Vacancy.ID != w.vacancy.ID || d.Vacancy.Title != w.vacancy.Title || d.Vacancy.OrgSlug != w.tm.Slug || d.Vacancy.Status != "published" || d.Vacancy.Deadline == nil {
		t.Errorf("vacancy = %+v", d.Vacancy)
	}
	// Снимок профиля: то, что человек отправил, с контактом из отклика и без служебных полей.
	p := d.Profile
	if p.Name != "Мария Смирнова" || p.Headline == "" || p.ContactEmail != "maria.apply@example.ru" || p.Visibility != "" || len(p.Specialties) != 1 {
		t.Errorf("profile snapshot = %+v", p)
	}
	// Файлы: резюме и два приложенных, имена очищены.
	if d.CV == nil || d.CV.Name != "Мария Смирнова — CV.pdf" || d.CV.Size < 100 {
		t.Errorf("cv = %+v", d.CV)
	}
	if len(d.Files) != 2 || d.Files[0].Name != "Список публикаций.pdf" || d.Files[1].Name != "diploma.pdf" {
		t.Errorf("files = %+v", d.Files)
	}
	// Рекомендатели получили письма со ссылками, соискатель видит их статусы.
	refs, ok := d.References.([]references.Request)
	if !ok || len(refs) != 2 || refs[0].Status != references.StatusPending {
		t.Fatalf("references = %#v", d.References)
	}
	for _, r := range []references.RefereeInput{ref1, ref2} {
		if mailCount(t, strings.ToLower(r.Email)) != 1 {
			t.Errorf("referee %s got %d mails", r.Email, mailCount(t, strings.ToLower(r.Email)))
		}
	}
	// Уведомления: организации (кому положено), соискателю — квитанция.
	tm := w.tm
	for name, c := range map[string]struct {
		who  testkit.Person
		want int
	}{"owner": {tm.Owner, 1}, "hr": {tm.HR, 1}, "headA": {tm.HeadA, 1}, "headB": {tm.HeadB, 0}, "outsider": {tm.Out, 0}} {
		if got := notificationsOf(t, c.who, "application_received"); got != c.want {
			t.Errorf("%s: %d notifications, want %d", name, got, c.want)
		}
	}
	if notificationsOf(t, me, "application_sent") != 1 {
		t.Error("the applicant did not get a receipt")
	}
	if mailCount(t, tm.Owner.Email) != 1 || mailCount(t, tm.HeadB.Email) != 0 {
		t.Errorf("mails: owner %d, headB %d", mailCount(t, tm.Owner.Email), mailCount(t, tm.HeadB.Email))
	}
	var link string
	if err := testkit.Pool.QueryRow(bg, `SELECT link FROM notifications WHERE user_id = $1 AND kind = 'application_received'`, tm.HR.ID).Scan(&link); err != nil || link != "/candidates/"+d.ID.String() {
		t.Errorf("link = %q %v", link, err)
	}
	// Вид файлов в базе.
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1 AND kind = 'cv'`, d.ID); n != 1 {
		t.Errorf("%d cv files", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1 AND kind = 'attachment'`, d.ID); n != 2 {
		t.Errorf("%d attachments", n)
	}
}

func TestHiddenProfileCanStillApplyAndStaysHidden(t *testing.T) {
	// D-072: человек сам откликнулся, организация видит отправленное; сам режим приватности профиля не меняется.
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	if _, err := w.Prof.SetPrivacy(bg, me.User, "hidden", false); err != nil {
		t.Fatal(err)
	}
	d := w.mustApply(me, nil)
	if d.Profile.Headline == "" {
		t.Fatal("hidden profile was not sent with the application")
	}
	staff, err := w.svc.Get(bg, w.tm.Owner.User, d.ID)
	if err != nil || staff.Profile.Headline == "" || staff.Viewer.Role != RoleStaff {
		t.Fatalf("staff view: %+v %v", staff.Profile, err)
	}
	own, _ := w.Prof.Own(bg, me.User)
	if own.Profile.Visibility != "hidden" {
		t.Errorf("the application changed the privacy mode to %q", own.Profile.Visibility)
	}
	// Страница профиля по-прежнему закрыта для организации: отклик не открывает профиль целиком.
	if _, err := w.Prof.Get(bg, own.Profile.ID, &w.tm.Owner.User); !errors.Is(err, profiles.ErrNotFound) {
		t.Errorf("profile page of a hidden profile: %v", err)
	}
}

func TestSnapshotDoesNotChangeWithTheProfile(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	d := w.mustApply(me, nil)
	old := d.Profile.Headline
	if _, err := w.Prof.SaveCore(bg, me.User, profiles.CoreInput{Headline: "Совсем другая должность", About: "Другой текст"}); err != nil {
		t.Fatal(err)
	}
	staff, err := w.svc.Get(bg, w.tm.Owner.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if staff.Profile.Headline != old || staff.Profile.About == "Другой текст" {
		t.Errorf("snapshot changed with the profile: %q", staff.Profile.Headline)
	}
}

func TestApplyValidation(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	cover := func(n int) func(*Input) {
		return func(in *Input) { in.CoverLetter = strings.Repeat("я", n) }
	}
	three := func(in *Input) {
		in.Referees = []references.RefereeInput{refereeIn(), refereeIn(), refereeIn(), refereeIn()}
	}
	cases := []struct {
		name  string
		mod   func(*Input)
		ups   []files.Upload
		field string
	}{
		{"no contact", func(in *Input) { in.ContactEmail = "" }, nil, "contact_email"},
		{"bad contact", func(in *Input) { in.ContactEmail = "не почта" }, nil, "contact_email"},
		{"cover empty", cover(0), nil, "cover_letter"},
		{"cover blank", func(in *Input) { in.CoverLetter = "   \n  " }, nil, "cover_letter"},
		{"cover 19", cover(19), nil, "cover_letter"},
		{"cover 6001", cover(6001), nil, "cover_letter"},
		{"referees 4", three, nil, "referees"},
		{"referee own email", func(in *Input) {
			in.Referees = []references.RefereeInput{{Name: "Я Сама", Email: me.Email}}
		}, nil, "referees"},
		{"referee equals contact", func(in *Input) {
			in.ContactEmail = "contact@example.ru"
			in.Referees = []references.RefereeInput{{Name: "Тот Же Человек", Email: "contact@example.ru"}}
		}, nil, "referees"},
		{"referee bad", func(in *Input) { in.Referees = []references.RefereeInput{{Name: "", Email: "x"}} }, nil, "referees"},
		{"file not a pdf", nil, []files.Upload{{Name: "virus.pdf", Data: []byte("MZ\x90\x00")}}, "files"},
		{"file empty", nil, []files.Upload{{Name: "empty.pdf"}}, "files"},
		{"six files", nil, []files.Upload{pdf("1"), pdf("2"), pdf("3"), pdf("4"), pdf("5"), pdf("6")}, "files"},
		{"file too big", nil, []files.Upload{{Name: "big.pdf", Data: append([]byte("%PDF-"), make([]byte, files.MaxFileSize)...)}}, "files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID)
			rates := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'apply' AND key = $1`, me.ID.String())
			_, err := w.apply(me, c.mod, c.ups...)
			var v *auth.ValidationError
			if !errors.As(err, &v) || v.Fields[c.field] == "" {
				t.Fatalf("err = %v", err)
			}
			if testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID) != before {
				t.Error("an invalid application was stored")
			}
			if testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'apply' AND key = $1`, me.ID.String()) != rates {
				t.Error("an invalid application spent the daily limit")
			}
		})
	}
	// Граничные значения проходят.
	for _, n := range []int{20, 6000} {
		other := w.Applicant("Граница")
		if _, err := w.apply(other, cover(n)); err != nil {
			t.Errorf("cover of %d characters: %v", n, err)
		}
	}
	// Five files pass.
	other := w.Applicant("Пять файлов")
	if _, err := w.apply(other, nil, pdf("1"), pdf("2"), pdf("3"), pdf("4"), pdf("5")); err != nil {
		t.Errorf("five files: %v", err)
	}
}

func TestApplyAllFieldErrorsAtOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	_, err := w.apply(me, func(in *Input) { in.ContactEmail = "x"; in.CoverLetter = "коротко" }, files.Upload{Name: "a.pdf", Data: []byte("nope")})
	var v *auth.ValidationError
	if !errors.As(err, &v) || len(v.Fields) != 3 {
		t.Errorf("fields = %v", v)
	}
}

func TestApplyNeedsAFilledProfile(t *testing.T) {
	w := newWorld(t)
	empty := w.User("Без Профиля")
	_, err := w.apply(empty, nil)
	var v *auth.ValidationError
	if !errors.As(err, &v) || v.Fields["profile"] == "" {
		t.Fatalf("err = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, empty.ID); n != 0 {
		t.Error("an application with an empty profile was stored")
	}
}

func TestApplyDeniedWhereItMakesNoSense(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")

	// Вакансии в разных состояниях.
	mk := func(unit *uuid.UUID) vacancies.Detail { return w.tm.Published(unit) }
	closed := mk(&w.tm.UnitA.ID)
	w.tm.Status(closed.ID, vacancies.StatusClosed)
	archived := mk(&w.tm.UnitA.ID)
	w.tm.Status(archived.ID, vacancies.StatusClosed)
	w.tm.Status(archived.ID, vacancies.StatusArchived)
	draft, err := w.Vac.Create(bg, w.tm.Owner.User, w.tm.Slug, testkit.VacancyInput(&w.tm.UnitA.ID))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		id   uuid.UUID
		want error
	}{
		"closed":   {closed.ID, ErrVacancyClosed},
		"archived": {archived.ID, ErrNotFound},
		"draft":    {draft.ID, ErrNotFound},
		"unknown":  {uuid.New(), ErrNotFound},
	} {
		_, err := w.apply(me, func(in *Input) { in.VacancyID = c.id })
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID); n != 0 {
		t.Errorf("%d applications stored", n)
	}
}

func TestDeadlineIsTheLastDayInclusiveByMoscowTime(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	deadline := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET deadline = $2 WHERE id = $1`, w.vacancy.ID, deadline); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		now  string
		want error
	}{
		{"2026-11-30T12:00:00Z", nil},
		{"2026-12-01T00:00:00Z", nil},               // 03:00 по Москве, последний день
		{"2026-12-01T20:59:59Z", nil},               // 23:59:59 по Москве
		{"2026-12-01T21:00:00Z", ErrDeadlinePassed}, // полночь по Москве: 2 декабря
		{"2026-12-05T12:00:00Z", ErrDeadlinePassed},
	}
	for _, c := range cases {
		w.clock.Set(mustTime(c.now))
		who := me
		if c.want == nil {
			who = w.Applicant("Подаёт в " + c.now) // каждый раз новый человек: на вакансию можно откликнуться один раз
		}
		_, err := w.apply(who, nil)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.now, err, c.want)
		}
	}
	// Вакансия без срока принимает всегда.
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET deadline = NULL WHERE id = $1`, w.vacancy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.apply(w.Applicant("Без срока"), nil); err != nil {
		t.Errorf("no deadline: %v", err)
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Кто ведёт вакансию и видит отклики на неё, откликаться на неё не может; остальным можно.
func TestOwnVacancyCannotBeAppliedTo(t *testing.T) {
	w := newWorld(t)
	tm := w.tm
	onWhole := tm.Published(nil) // вакансия на всю организацию
	cases := []struct {
		name    string
		who     testkit.Person
		vacancy uuid.UUID
		denied  bool
	}{
		{"owner", tm.Owner, w.vacancy.ID, true},
		{"hr", tm.HR, w.vacancy.ID, true},
		{"head of the same unit", tm.HeadA, w.vacancy.ID, true},
		{"head of another unit", tm.HeadB, w.vacancy.ID, false},
		{"outsider", tm.Out, w.vacancy.ID, false},
		{"owner, whole-organization vacancy", tm.Owner, onWhole.ID, true},
		{"hr, whole-organization vacancy", tm.HR, onWhole.ID, true},
		{"head, whole-organization vacancy", tm.HeadA, onWhole.ID, false},
	}
	for _, c := range cases {
		w.FillProfile(c.who)
		_, err := w.apply(c.who, func(in *Input) { in.VacancyID = c.vacancy })
		if c.denied && !errors.Is(err, ErrOwnVacancy) {
			t.Errorf("%s: %v, want ErrOwnVacancy", c.name, err)
		}
		if !c.denied && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestOnlyOneActiveApplicationPerVacancy(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	if _, err := w.apply(me, nil); !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("second application: %v", err)
	}
	if err := w.svc.Withdraw(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	// После отзыва можно откликнуться заново, это новый отклик.
	again, err := w.apply(me, nil)
	if err != nil || again.ID == d.ID {
		t.Fatalf("apply after withdraw: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1 AND vacancy_id = $2`, me.ID, w.vacancy.ID); n != 2 {
		t.Errorf("%d applications", n)
	}
	// Отклонённый отклик тоже мешает повторному: решение уже принято.
	other := w.Applicant("Другая")
	rej := w.mustApply(other, nil)
	w.setStatus(rej.ID, StatusRejected)
	if _, err := w.apply(other, nil); !errors.Is(err, ErrAlreadyApplied) {
		t.Errorf("after rejection: %v", err)
	}
}

func TestSimultaneousApplicationsCreateOne(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.apply(me, func(in *Input) { in.Referees = []references.RefereeInput{refereeIn()} })
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrAlreadyApplied):
			t.Errorf("unexpected: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d applications succeeded", wins)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID); n != 1 {
		t.Errorf("%d applications stored", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM reference_requests r JOIN applications a ON a.id = r.application_id WHERE a.user_id = $1`, me.ID); n != 1 {
		t.Errorf("%d referee requests stored", n)
	}
	if n := notificationsOf(t, w.tm.Owner, "application_received"); n != 1 {
		t.Errorf("owner got %d notifications", n)
	}
}

func TestApplyRateLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Apply = Limit{Max: 2, Window: time.Hour}
	me := w.Applicant("Мария")
	for range 2 {
		v := w.tm.Published(&w.tm.UnitA.ID)
		if _, err := w.apply(me, func(in *Input) { in.VacancyID = v.ID }); err != nil {
			t.Fatal(err)
		}
	}
	v := w.tm.Published(&w.tm.UnitA.ID)
	_, err := w.apply(me, func(in *Input) { in.VacancyID = v.ID })
	var rl *auth.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("third: %v", err)
	}
	w.clock.Advance(time.Hour + time.Minute)
	if _, err := w.apply(me, func(in *Input) { in.VacancyID = v.ID }); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

// ---- кто что видит ----

func TestWhoSeesTheApplication(t *testing.T) {
	w := newWorld(t)
	tm := w.tm
	me := w.Applicant("Мария Смирнова")
	d := w.mustApply(me, nil, pdf("Список.pdf"))
	// Приходит письмо рекомендателя.
	req, err := w.refs.Add(bg, me.User, d.ID, refereeIn())
	if err != nil {
		t.Fatal(err)
	}
	_ = req

	cases := []struct {
		name string
		who  testkit.Person
		role string // пусто — отклика для этого человека нет
	}{
		{"applicant", me, RoleApplicant},
		{"owner", tm.Owner, RoleStaff},
		{"hr", tm.HR, RoleStaff},
		{"head of the unit", tm.HeadA, RoleStaff},
		{"head of another unit", tm.HeadB, ""},
		{"outsider", tm.Out, ""},
		{"another applicant", w.Applicant("Другая"), ""},
	}
	for _, c := range cases {
		got, err := w.svc.Get(bg, c.who.User, d.ID)
		if c.role == "" {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("%s: %v, want ErrNotFound", c.name, err)
			}
			if got.ID != uuid.Nil || got.CoverLetter != "" {
				t.Errorf("%s: data leaked with the refusal: %+v", c.name, got)
			}
			continue
		}
		if err != nil || got.Viewer.Role != c.role {
			t.Errorf("%s: %v %+v", c.name, err, got.Viewer)
		}
	}
	if _, err := w.svc.Get(bg, me.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestLettersAreVisibleToOrganizationOnly(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	in := refereeIn()
	d := w.mustApply(me, func(i *Input) { i.Referees = []references.RefereeInput{in} })
	token := w.tokenFor(in.Email)
	if err := w.refs.Submit(bg, token, references.LetterInput{Text: "ТЕКСТ РЕКОМЕНДАЦИИ"}, &files.Upload{Name: "Письмо.pdf", Data: pdfBytes()}); err != nil {
		t.Fatal(err)
	}

	mine, err := w.svc.Get(bg, me.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mine)
	for _, banned := range []string{"ТЕКСТ РЕКОМЕНДАЦИИ", "Письмо.pdf", `"letter"`, "reference_letter"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("applicant sees %q: %s", banned, raw)
		}
	}
	reqs := mine.References.([]references.Request)
	if len(reqs) != 1 || reqs[0].Status != references.StatusReceived {
		t.Errorf("applicant's references = %+v", reqs)
	}
	if len(mine.Files) != 0 || mine.CV == nil {
		t.Errorf("the letter file is listed among the applicant's files: %+v", mine.Files)
	}

	for name, who := range map[string]testkit.Person{"owner": w.tm.Owner, "hr": w.tm.HR, "head": w.tm.HeadA} {
		staff, err := w.svc.Get(bg, who.User, d.ID)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sreqs, ok := staff.References.([]references.StaffRequest)
		if !ok || len(sreqs) != 1 || sreqs[0].Letter == nil || sreqs[0].Letter.Text != "ТЕКСТ РЕКОМЕНДАЦИИ" || sreqs[0].Letter.File == nil {
			t.Errorf("%s does not see the letter: %#v", name, staff.References)
		}
		if len(staff.Files) != 0 {
			t.Errorf("%s: the letter is listed among the application files", name)
		}
	}
}

// tokenFor достаёт ссылку из письма рекомендателю.
func (w *world) tokenFor(email string) string {
	w.T.Helper()
	var body string
	if err := testkit.Pool.QueryRow(bg, `SELECT body FROM outbox WHERE to_email = $1 ORDER BY id DESC LIMIT 1`, strings.ToLower(email)).Scan(&body); err != nil {
		w.T.Fatal(err)
	}
	i := strings.Index(body, "/recommend?token=")
	rest := body[i+len("/recommend?token="):]
	if j := strings.IndexAny(rest, " \n\r"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// Человек, который откликнулся, а потом стал сотрудником этой же организации, письма о себе не видит.
func TestApplicantWhoBecomesStaffStillSeesTheApplicantView(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	in := refereeIn()
	d := w.mustApply(me, func(i *Input) { i.Referees = []references.RefereeInput{in} })
	if err := w.refs.Submit(bg, w.tokenFor(in.Email), references.LetterInput{Text: "Письмо о ней"}, &files.Upload{Name: "l.pdf", Data: pdfBytes()}); err != nil {
		t.Fatal(err)
	}
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: w.tm.OrgID, UserID: me.ID, Role: "hr", JoinedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.Get(bg, me.User, d.ID)
	if err != nil || got.Viewer.Role != RoleApplicant {
		t.Fatalf("view: %v %+v", err, got.Viewer)
	}
	if _, ok := got.References.([]references.Request); !ok {
		t.Errorf("references of the applicant-turned-staff: %T", got.References)
	}
	// И файл письма он тоже получить не может.
	staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID)
	letter := staff.References.([]references.StaffRequest)[0].Letter.File
	if _, _, err := w.svc.File(bg, me.User, d.ID, letter.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("letter file: %v", err)
	}
}

func TestFileAccess(t *testing.T) {
	w := newWorld(t)
	tm := w.tm
	me := w.Applicant("Мария")
	in := refereeIn()
	d := w.mustApply(me, func(i *Input) { i.Referees = []references.RefereeInput{in} }, pdf("Приложение.pdf"))
	if err := w.refs.Submit(bg, w.tokenFor(in.Email), references.LetterInput{}, &files.Upload{Name: "Письмо.pdf", Data: pdfBytes()}); err != nil {
		t.Fatal(err)
	}
	staff, _ := w.svc.Get(bg, tm.Owner.User, d.ID)
	letterID := staff.References.([]references.StaffRequest)[0].Letter.File.ID
	files := map[string]uuid.UUID{"cv": d.CV.ID, "attachment": d.Files[0].ID, "letter": letterID}

	// Строки: кто; столбцы: резюме, приложение, письмо рекомендателя.
	cases := []struct {
		name string
		who  testkit.Person
		want map[string]bool
	}{
		{"applicant", me, map[string]bool{"cv": true, "attachment": true, "letter": false}},
		{"owner", tm.Owner, map[string]bool{"cv": true, "attachment": true, "letter": true}},
		{"hr", tm.HR, map[string]bool{"cv": true, "attachment": true, "letter": true}},
		{"head of the unit", tm.HeadA, map[string]bool{"cv": true, "attachment": true, "letter": true}},
		{"head of another unit", tm.HeadB, map[string]bool{"cv": false, "attachment": false, "letter": false}},
		{"outsider", tm.Out, map[string]bool{"cv": false, "attachment": false, "letter": false}},
	}
	for _, c := range cases {
		for kind, id := range files {
			name, data, err := w.svc.File(bg, c.who.User, d.ID, id)
			if c.want[kind] {
				if err != nil || len(data) == 0 || !strings.HasSuffix(name, ".pdf") {
					t.Errorf("%s/%s: %v", c.name, kind, err)
				}
			} else if !errors.Is(err, ErrNotFound) || len(data) != 0 {
				t.Errorf("%s/%s: %v (must be not found, no data)", c.name, kind, err)
			}
		}
	}
	// Файл чужого отклика по адресу своего отклика получить нельзя.
	other := w.Applicant("Другая")
	d2 := w.mustApply(other, nil, pdf("Другое.pdf"))
	if _, _, err := w.svc.File(bg, other.User, d2.ID, d.Files[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("file of another application: %v", err)
	}
	if _, _, err := w.svc.File(bg, me.User, d.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown file: %v", err)
	}
	if _, _, err := w.svc.File(bg, me.User, uuid.New(), d.CV.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
}

func TestUnknownFileKindIsNeverServed(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	// Строку с неизвестным видом база не пропустит (CHECK), поэтому проверяем, что она и правда не пропускает.
	_, err := testkit.Pool.Exec(bg, `INSERT INTO application_files (application_id, kind, name, size, data, created_at) VALUES ($1, 'exe', 'x.pdf', 1, 'x', now())`, d.ID)
	if err == nil {
		t.Fatal("the database accepted an unknown file kind")
	}
}

// ---- отзыв ----

func TestWithdraw(t *testing.T) {
	for status, ok := range map[string]bool{StatusSent: true, StatusViewed: true, StatusInvited: true, StatusRejected: false, StatusAccepted: false, StatusWithdrawn: false} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария Смирнова")
			d := w.mustApply(me, nil)
			w.setStatus(d.ID, status)
			err := w.svc.Withdraw(bg, me.User, d.ID)
			if ok {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := w.svc.Get(bg, me.User, d.ID)
				if got.Status != StatusWithdrawn || got.Viewer.CanWithdraw {
					t.Errorf("after withdraw: %s %+v", got.Status, got.Viewer)
				}
				for name, who := range map[string]testkit.Person{"owner": w.tm.Owner, "hr": w.tm.HR, "head": w.tm.HeadA} {
					if notificationsOf(t, who, "application_withdrawn") != 1 {
						t.Errorf("%s was not told about the withdrawal", name)
					}
				}
				if notificationsOf(t, w.tm.HeadB, "application_withdrawn") != 0 {
					t.Error("another unit's head was told")
				}
			} else if !errors.Is(err, ErrBadStatus) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestWithdrawOnlyByTheApplicant(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	for name, who := range map[string]testkit.Person{"owner": w.tm.Owner, "hr": w.tm.HR, "outsider": w.tm.Out} {
		if err := w.svc.Withdraw(bg, who.User, d.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := w.svc.Withdraw(bg, me.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	got, _ := w.svc.Get(bg, me.User, d.ID)
	if got.Status != StatusSent {
		t.Errorf("a refused withdrawal changed the status to %s", got.Status)
	}
}

func TestWithdrawKillsRefereeLinks(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	in := refereeIn()
	d := w.mustApply(me, func(i *Input) { i.Referees = []references.RefereeInput{in} })
	token := w.tokenFor(in.Email)
	if err := w.svc.Withdraw(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.refs.Submit(bg, token, references.LetterInput{Text: "поздно"}, nil); !errors.Is(err, references.ErrGone) {
		t.Errorf("letter after withdrawal: %v", err)
	}
	if _, err := w.refs.Add(bg, me.User, d.ID, refereeIn()); !errors.Is(err, references.ErrClosed) {
		t.Errorf("new referee after withdrawal: %v", err)
	}
}

func TestSimultaneousWithdrawalsNotifyOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = w.svc.Withdraw(bg, me.User, d.ID) }()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrBadStatus) {
			t.Errorf("unexpected: %v", err)
		}
	}
	if wins != 1 || notificationsOf(t, w.tm.Owner, "application_withdrawn") != 1 {
		t.Errorf("wins=%d notifications=%d", wins, notificationsOf(t, w.tm.Owner, "application_withdrawn"))
	}
}

// ---- списки ----

func TestMine(t *testing.T) {
	w := newWorld(t)
	me, other := w.Applicant("Мария"), w.Applicant("Другая")
	var ids []uuid.UUID
	for i := range 3 {
		v := w.tm.Published(&w.tm.UnitA.ID)
		w.clock.Advance(time.Minute)
		in := refereeIn()
		var mod func(*Input)
		if i == 0 {
			mod = func(x *Input) { x.VacancyID = v.ID; x.Referees = []references.RefereeInput{in} }
		} else {
			mod = func(x *Input) { x.VacancyID = v.ID }
		}
		d := w.mustApply(me, mod)
		ids = append(ids, d.ID)
		if i == 0 {
			if err := w.refs.Submit(bg, w.tokenFor(in.Email), references.LetterInput{Text: "письмо"}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.mustApply(other, nil)

	l, err := w.svc.Mine(bg, me.User, 0, 0)
	if err != nil || l.Total != 3 || len(l.Items) != 3 {
		t.Fatalf("mine = %+v %v", l, err)
	}
	// Новые сверху.
	if l.Items[0].ID != ids[2] || l.Items[2].ID != ids[0] {
		t.Errorf("order: %v", []uuid.UUID{l.Items[0].ID, l.Items[1].ID, l.Items[2].ID})
	}
	last := l.Items[2]
	if last.References.Total != 1 || last.References.Received != 1 || last.Vacancy.Title == "" || last.Vacancy.OrgName == "" || last.Status != StatusSent {
		t.Errorf("summary = %+v", last)
	}
	if l.Items[0].References.Total != 0 {
		t.Errorf("references of the newest = %+v", l.Items[0].References)
	}
	// Страницы.
	p2, _ := w.svc.Mine(bg, me.User, 2, 2)
	if len(p2.Items) != 1 || p2.Items[0].ID != ids[0] || p2.Total != 3 {
		t.Errorf("page 2 = %+v", p2)
	}
	huge, _ := w.svc.Mine(bg, me.User, 100000, -4)
	if len(huge.Items) != 3 {
		t.Errorf("huge limit page has %d items", len(huge.Items))
	}
	// Чужих откликов в списке нет.
	if l2, _ := w.svc.Mine(bg, w.tm.Out.User, 20, 0); l2.Total != 0 || len(l2.Items) != 0 {
		t.Errorf("outsider's list = %+v", l2)
	}
}

func TestForVacancy(t *testing.T) {
	w := newWorld(t)
	tm := w.tm
	me := w.Applicant("Мария")
	closed := tm.Published(&tm.UnitA.ID)
	tm.Status(closed.ID, vacancies.StatusClosed)
	draft, _ := w.Vac.Create(bg, tm.Owner.User, tm.Slug, testkit.VacancyInput(&tm.UnitA.ID))

	state := func(who testkit.Person, id uuid.UUID) VacancyState {
		t.Helper()
		s, err := w.svc.ForVacancy(bg, who.User, id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := state(me, w.vacancy.ID); !s.CanApply || s.Reason != "" || s.Application != nil {
		t.Errorf("open: %+v", s)
	}
	if s := state(tm.Owner, w.vacancy.ID); s.CanApply || s.Reason != ReasonOwn {
		t.Errorf("owner: %+v", s)
	}
	if s := state(tm.HeadB, w.vacancy.ID); !s.CanApply {
		t.Errorf("another unit's head: %+v", s)
	}
	if s := state(me, closed.ID); s.CanApply || s.Reason != ReasonClosed {
		t.Errorf("closed: %+v", s)
	}
	// Срок прошёл.
	w.clock.Set(time.Now().UTC().AddDate(1, 0, 0))
	if s := state(me, w.vacancy.ID); s.CanApply || s.Reason != ReasonExpired {
		t.Errorf("expired: %+v", s)
	}
	w.clock.Set(time.Now().UTC())
	for name, id := range map[string]uuid.UUID{"draft": draft.ID, "unknown": uuid.New()} {
		if _, err := w.svc.ForVacancy(bg, me.User, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	d := w.mustApply(me, nil)
	if s := state(me, w.vacancy.ID); s.CanApply || s.Reason != ReasonApplied || s.Application == nil || s.Application.ID != d.ID || s.Application.Status != StatusSent {
		t.Errorf("applied: %+v", s)
	}
	// Отозвал — можно снова.
	if err := w.svc.Withdraw(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if s := state(me, w.vacancy.ID); !s.CanApply || s.Application != nil {
		t.Errorf("after withdraw: %+v", s)
	}
	// Другой человек откликов этого не видит.
	if s := state(tm.Out, w.vacancy.ID); s.Application != nil {
		t.Errorf("outsider sees someone else's application: %+v", s)
	}
}

func TestCanWithdrawTable(t *testing.T) {
	want := map[string]bool{StatusSent: true, StatusViewed: true, StatusInvited: true, StatusRejected: false, StatusAccepted: false, StatusWithdrawn: false, "": false, "unknown": false}
	for s, ok := range want {
		if CanWithdraw(s) != ok {
			t.Errorf("CanWithdraw(%q) = %v", s, !ok)
		}
	}
	if len(Statuses) != 6 {
		t.Errorf("%d statuses", len(Statuses))
	}
}

func TestCorruptSnapshotIsReported(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET profile = '"text"' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, me.User, d.ID); err == nil {
		t.Error("a damaged snapshot must give an error, not an empty profile")
	}
}

func TestNotificationsInTheSameTransaction(t *testing.T) {
	// Если отклик не записался, уведомлений и писем тоже нет.
	w := newWorld(t)
	me := w.Applicant("Мария")
	before := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, w.tm.Owner.ID)
	in := refereeIn()
	_, err := w.apply(me, func(i *Input) {
		i.ContactEmail = "not an email"
		i.Referees = []references.RefereeInput{in}
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, w.tm.Owner.ID); got != before {
		t.Error("a failed application notified the organization")
	}
	if mailCount(t, strings.ToLower(in.Email)) != 0 {
		t.Error("a failed application mailed a referee")
	}
}

// brokenProfiles и brokenRefs возвращают ошибку, как будто их собственная база недоступна.
type brokenProfiles struct{ Profiles }

func (brokenProfiles) ForApplication(context.Context, auth.User, string) (profiles.ApplicationPackage, error) {
	return profiles.ApplicationPackage{}, errBroken
}

type brokenRefs struct {
	References
	applicant, staff bool
}

func (b brokenRefs) ListForApplicant(ctx context.Context, id uuid.UUID) ([]references.Request, error) {
	if b.applicant {
		return nil, errBroken
	}
	return b.References.ListForApplicant(ctx, id)
}

func (b brokenRefs) ListForStaff(ctx context.Context, id uuid.UUID) ([]references.StaffRequest, error) {
	if b.staff {
		return nil, errBroken
	}
	return b.References.ListForStaff(ctx, id)
}

var errBroken = errors.New("dependency is down")

func TestFailuresOfDependenciesAreReported(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)

	s := newService(testkit.Pool, brokenProfiles{w.Prof}, w.refs, w.notes, testConfig())
	other := w.Applicant("Другая")
	if _, err := s.Apply(bg, other.User, w.input(other), nil); !errors.Is(err, errBroken) {
		t.Errorf("profile failure: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, other.ID); n != 0 {
		t.Error("an application was stored although the profile could not be read")
	}
	s = newService(testkit.Pool, w.Prof, brokenRefs{References: w.refs, applicant: true}, w.notes, testConfig())
	if _, err := s.Get(bg, me.User, d.ID); !errors.Is(err, errBroken) {
		t.Errorf("references failure (applicant): %v", err)
	}
	s = newService(testkit.Pool, w.Prof, brokenRefs{References: w.refs, staff: true}, w.notes, testConfig())
	if _, err := s.Get(bg, w.tm.Owner.User, d.ID); !errors.Is(err, errBroken) {
		t.Errorf("references failure (staff): %v", err)
	}
}
