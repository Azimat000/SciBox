package references

import (
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
	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
)

func TestAddSendsOneTimeLinkToTheReferee(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	app := w.application(me)
	in := RefereeInput{Name: "  Профессор   Иванов ", Email: "Ivanov." + emailSeq(), Relation: "научный руководитель"}
	req, err := w.svc.Add(bg, me.User, app, in)
	if err != nil {
		t.Fatal(err)
	}
	if req.Name != "Профессор Иванов" || req.Status != StatusPending || req.Relation != "научный руководитель" || req.AnsweredAt != nil {
		t.Errorf("request = %+v", req)
	}
	if req.CanResend || req.ResendAt == nil {
		t.Errorf("a request just sent must not be resendable yet: %+v", req)
	}
	ms := mailsTo(t, strings.ToLower(in.Email))
	if len(ms) != 1 {
		t.Fatalf("mails = %d", len(ms))
	}
	m := ms[0]
	for _, want := range []string{"Профессор Иванов", "Мария Смирнова", "«" + "Старший научный сотрудник в лабораторию катализа" + "»", "Регистрироваться не нужно", "кандидат письмо не увидит", "срабатывает один раз", "/recommend?token="} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("mail lacks %q:\n%s", want, m.Body)
		}
	}
	if !strings.Contains(m.Subject, "Мария Смирнова") {
		t.Errorf("subject = %q", m.Subject)
	}
	// Ссылка в письме открывает именно эту просьбу.
	info, err := w.svc.Lookup(bg, tokenFrom(t, strings.ToLower(in.Email)))
	if err != nil {
		t.Fatal(err)
	}
	if info.RefereeName != "Профессор Иванов" || info.ApplicantName != "Мария Смирнова" || info.Status != StatusPending || info.OrgName == "" {
		t.Errorf("info = %+v", info)
	}
	// В базе хранится только хеш ссылки.
	token := tokenFrom(t, strings.ToLower(in.Email))
	if got := testkit.Count(t, `SELECT count(*) FROM reference_requests WHERE encode(token_hash, 'escape') = $1`, token); got != 0 {
		t.Error("the raw token is stored in the database")
	}
}

func TestAddChecksWhoAsksAndWhatState(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	other := w.Applicant("Другая")

	// Чужой отклик и сотрудники организации не могут просить от имени соискателя: отклика для них «нет».
	for name, who := range map[string]testkit.Person{"stranger": other, "owner": w.tm.Owner, "hr": w.tm.HR} {
		if _, err := w.svc.Add(bg, who.User, app, referee()); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := w.svc.Add(bg, me.User, uuid.New(), referee()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
	// По статусу отклика: пока решения нет, можно; после решения или отзыва нельзя.
	for status, ok := range map[string]bool{"sent": true, "viewed": true, "invited": true, "rejected": false, "accepted": false, "withdrawn": false} {
		own := w.Applicant("Владелец " + status)
		id := w.application(own)
		w.setStatus(id, status)
		_, err := w.svc.Add(bg, own.User, id, referee())
		if ok && err != nil || !ok && !errors.Is(err, ErrClosed) {
			t.Errorf("status %s: %v", status, err)
		}
	}
}

func TestAddValidatesAndLimits(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)

	fields := func(err error) map[string]string {
		var v *auth.ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("not a validation error: %v", err)
		}
		return v.Fields
	}
	_, err := w.svc.Add(bg, me.User, app, RefereeInput{Name: "", Email: "плохая почта"})
	if f := fields(err); f["name"] == "" || f["email"] == "" {
		t.Errorf("fields = %v", f)
	}
	_, err = w.svc.Add(bg, me.User, app, RefereeInput{Name: "Иван Петров", Email: me.Email})
	if f := fields(err); f["email"] != msgSelf {
		t.Errorf("own account email: %v", f)
	}
	first := referee()
	if _, err := w.svc.Add(bg, me.User, app, first); err != nil {
		t.Fatal(err)
	}
	_, err = w.svc.Add(bg, me.User, app, RefereeInput{Name: "Тот же человек", Email: strings.ToUpper(first.Email)})
	if f := fields(err); f["email"] != msgDuplicate {
		t.Errorf("duplicate: %v", f)
	}
	for range MaxPerApplication - 1 {
		if _, err := w.svc.Add(bg, me.User, app, referee()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.svc.Add(bg, me.User, app, referee()); !errors.Is(err, ErrTooMany) {
		t.Errorf("fourth referee: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM reference_requests WHERE application_id = $1`, app); n != MaxPerApplication {
		t.Errorf("%d requests stored", n)
	}
}

func TestRequestRateLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Requests = Limit{Max: 2, Window: time.Hour}
	me := w.Applicant("Мария")
	app := w.application(me)
	for range 2 {
		w.add(me, app)
	}
	_, err := w.svc.Add(bg, me.User, app, referee())
	var rl *auth.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > time.Hour {
		t.Fatalf("err = %v", err)
	}
	// Другому человеку лимит первого не мешает.
	other := w.Applicant("Другая")
	w.add(other, w.application(other))
	// Через час окно пройдёт.
	w.clock.Advance(time.Hour + time.Minute)
	if _, err := w.svc.Add(bg, me.User, app, referee()); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func TestLookupStates(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)

	if _, err := w.svc.Lookup(bg, "несуществующая-ссылка"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("unknown token: %v", err)
	}
	if _, err := w.svc.Lookup(bg, ""); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("empty token: %v", err)
	}
	_, token := w.add(me, app)
	if _, err := w.svc.Lookup(bg, token); err != nil {
		t.Fatal(err)
	}
	// Срок: до последней секунды работает, потом нет.
	w.clock.Advance(w.svc.cfg.TTL - time.Second)
	if _, err := w.svc.Lookup(bg, token); err != nil {
		t.Errorf("just before expiry: %v", err)
	}
	w.clock.Advance(time.Second)
	if _, err := w.svc.Lookup(bg, token); !errors.Is(err, ErrExpired) {
		t.Errorf("at expiry: %v", err)
	}

	// Отозванный отклик: ссылка больше не нужна.
	other := w.Applicant("Четвёртая")
	app2 := w.application(other)
	_, token3 := w.add(other, app2)
	w.setStatus(app2, "withdrawn")
	if _, err := w.svc.Lookup(bg, token3); !errors.Is(err, ErrGone) {
		t.Errorf("withdrawn application: %v", err)
	}
}

func TestSubmitTextPdfAndBoth(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		file     *files.Upload
		wantText string
		wantFile bool
	}{
		{"text only", "  Рекомендую без оговорок.  ", nil, "Рекомендую без оговорок.", false},
		{"pdf only", "", &files.Upload{Name: "C:\\Users\\Иванов\\Письмо.pdf", Data: pdfBytes()}, "", true},
		{"both", "Коротко: рекомендую.", &files.Upload{Name: "Письмо.pdf", Data: pdfBytes()}, "Коротко: рекомендую.", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария Смирнова")
			app := w.application(me)
			req, token := w.add(me, app)

			if err := w.svc.Submit(bg, token, LetterInput{Text: c.text}, c.file); err != nil {
				t.Fatal(err)
			}
			// Организация видит письмо.
			staff, err := w.svc.ListForStaff(bg, app)
			if err != nil || len(staff) != 1 {
				t.Fatalf("staff view: %v %v", staff, err)
			}
			sr := staff[0]
			if sr.ID != req.ID || sr.Status != StatusReceived || sr.AnsweredAt == nil || sr.Letter == nil || sr.Letter.Text != c.wantText {
				t.Fatalf("staff request = %+v letter=%+v", sr, sr.Letter)
			}
			if (sr.Letter.File != nil) != c.wantFile {
				t.Fatalf("file = %+v", sr.Letter.File)
			}
			if c.wantFile {
				if sr.Letter.File.Name != "Письмо.pdf" || sr.Letter.File.Size != len(pdfBytes()) {
					t.Errorf("file = %+v", sr.Letter.File)
				}
				var kind string
				var ref *uuid.UUID
				if err := testkit.Pool.QueryRow(bg, `SELECT kind, reference_id FROM application_files WHERE id = $1`, sr.Letter.File.ID).Scan(&kind, &ref); err != nil {
					t.Fatal(err)
				}
				if kind != "reference_letter" || ref == nil || *ref != req.ID {
					t.Errorf("stored as kind=%q ref=%v", kind, ref)
				}
			}
			// Соискатель видит только то, что письмо получено.
			mine, err := w.svc.ListForApplicant(bg, app)
			if err != nil || len(mine) != 1 || mine[0].Status != StatusReceived {
				t.Fatalf("applicant view: %+v %v", mine, err)
			}
			raw, _ := json.Marshal(mine)
			for _, banned := range []string{"letter", "Рекомендую", "Коротко", "reference_letter"} {
				if strings.Contains(string(raw), banned) {
					t.Errorf("applicant view leaks %q: %s", banned, raw)
				}
			}
		})
	}
}

func TestSubmitIsOneTime(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, token := w.add(me, app)
	if err := w.svc.Submit(bg, token, LetterInput{Text: "Первое письмо"}, nil); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"second submit": w.svc.Submit(bg, token, LetterInput{Text: "Второе письмо"}, nil),
		"decline after": w.svc.Decline(bg, token),
	} {
		if !errors.Is(err, ErrAlreadyAnswered) {
			t.Errorf("%s: %v", name, err)
		}
	}
	staff, _ := w.svc.ListForStaff(bg, app)
	if staff[0].Letter.Text != "Первое письмо" {
		t.Errorf("the first letter was overwritten: %q", staff[0].Letter.Text)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1 AND kind = 'reference_letter'`, app); n != 0 {
		t.Errorf("%d files", n)
	}
	// Страница по ссылке после ответа показывает «уже ответили», а не форму.
	info, err := w.svc.Lookup(bg, token)
	if err != nil || info.Status != StatusReceived {
		t.Errorf("lookup after answer: %+v %v", info, err)
	}
}

func TestSubmitConcurrentOnlyOneWins(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, token := w.add(me, app)
	var wg sync.WaitGroup
	results := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = w.svc.Submit(bg, token, LetterInput{Text: "Письмо"}, &files.Upload{Name: "a.pdf", Data: pdfBytes()})
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrAlreadyAnswered):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d submissions succeeded", wins)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1 AND kind = 'reference_letter'`, app); n != 1 {
		t.Errorf("%d letter files stored", n)
	}
	if n := notificationsOf(t, w.tm.Owner, "reference_received"); n != 1 {
		t.Errorf("owner got %d notifications", n)
	}
}

func TestSubmitValidation(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, token := w.add(me, app)

	fields := func(err error) map[string]string {
		var v *auth.ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("not a validation error: %v", err)
		}
		return v.Fields
	}
	if f := fields(w.svc.Submit(bg, token, LetterInput{Text: "  \n "}, nil)); f["text"] == "" {
		t.Errorf("empty letter: %v", f)
	}
	if f := fields(w.svc.Submit(bg, token, LetterInput{Text: strings.Repeat("я", MaxLetterRunes+1)}, nil)); f["text"] == "" {
		t.Errorf("long letter: %v", f)
	}
	if f := fields(w.svc.Submit(bg, token, LetterInput{Text: "Письмо"}, &files.Upload{Name: "a.pdf", Data: []byte("MZ not a pdf")})); f["file"] == "" {
		t.Errorf("not a pdf: %v", f)
	}
	if f := fields(w.svc.Submit(bg, token, LetterInput{}, &files.Upload{Name: "a.pdf"})); f["file"] == "" {
		t.Errorf("empty file: %v", f)
	}
	// Ошибки формы ссылку не тратят.
	if err := w.svc.Submit(bg, token, LetterInput{Text: "Теперь правильно"}, nil); err != nil {
		t.Errorf("link was spent by invalid attempts: %v", err)
	}
	// Ровно предельная длина проходит.
	_, token2 := w.add(me, app)
	if err := w.svc.Submit(bg, token2, LetterInput{Text: strings.Repeat("я", MaxLetterRunes)}, nil); err != nil {
		t.Errorf("letter of the maximum length: %v", err)
	}
}

func TestSubmitRejectsDeadLinks(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	if err := w.svc.Submit(bg, "нет-такой", LetterInput{Text: "x"}, nil); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("unknown: %v", err)
	}
	_, token := w.add(me, app)
	w.clock.Advance(w.svc.cfg.TTL)
	if err := w.svc.Submit(bg, token, LetterInput{Text: "поздно"}, nil); !errors.Is(err, ErrExpired) {
		t.Errorf("expired: %v", err)
	}
	other := w.Applicant("Другая")
	app2 := w.application(other)
	_, token2 := w.add(other, app2)
	w.setStatus(app2, "withdrawn")
	if err := w.svc.Submit(bg, token2, LetterInput{Text: "не нужно"}, nil); !errors.Is(err, ErrGone) {
		t.Errorf("withdrawn: %v", err)
	}
	if err := w.svc.Decline(bg, token2); !errors.Is(err, ErrGone) {
		t.Errorf("decline for a withdrawn application: %v", err)
	}
	// Ничего не записано.
	for _, id := range []uuid.UUID{app, app2} {
		if n := testkit.Count(t, `SELECT count(*) FROM reference_requests WHERE application_id = $1 AND status <> 'pending'`, id); n != 0 {
			t.Errorf("a dead link changed the request (%d)", n)
		}
	}
}

// Кому приходят уведомления о письме: тем, кто вправе видеть отклики на эту вакансию, и самому соискателю (без содержания).
func TestSubmitNotifiesTheRightPeople(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	app := w.application(me)
	_, token := w.add(me, app)
	if err := w.svc.Submit(bg, token, LetterInput{Text: "СЕКРЕТНОЕ СОДЕРЖАНИЕ ПИСЬМА"}, nil); err != nil {
		t.Fatal(err)
	}
	tm := w.tm
	want := map[string]int{"owner": 1, "hr": 1, "headA": 1, "headB": 0, "outsider": 0}
	got := map[string]int{
		"owner": notificationsOf(t, tm.Owner, "reference_received"), "hr": notificationsOf(t, tm.HR, "reference_received"),
		"headA": notificationsOf(t, tm.HeadA, "reference_received"), "headB": notificationsOf(t, tm.HeadB, "reference_received"),
		"outsider": notificationsOf(t, tm.Out, "reference_received"),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d notifications, want %d", k, got[k], v)
		}
	}
	if n := notificationsOf(t, me, "reference_received_mine"); n != 1 {
		t.Errorf("applicant got %d notifications", n)
	}
	// Содержание письма нигде в уведомлениях и письмах соискателя не встречается.
	if n := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND (title || body || link) LIKE '%СЕКРЕТНОЕ%'`, me.ID); n != 0 {
		t.Error("the letter text leaked into the applicant's notifications")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1 AND body LIKE '%СЕКРЕТНОЕ%'`, me.Email); n != 0 {
		t.Error("the letter text leaked into the applicant's mail")
	}
	// Организации письмо с ссылкой на карточку отклика.
	var link string
	if err := testkit.Pool.QueryRow(bg, `SELECT link FROM notifications WHERE user_id = $1 AND kind = 'reference_received'`, tm.Owner.ID).Scan(&link); err != nil || link != "/candidates/"+app.String() {
		t.Errorf("link = %q %v", link, err)
	}
}

func TestDecline(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Смирнова")
	app := w.application(me)
	_, token := w.add(me, app)
	if err := w.svc.Decline(bg, token); err != nil {
		t.Fatal(err)
	}
	mine, _ := w.svc.ListForApplicant(bg, app)
	if mine[0].Status != StatusDeclined || mine[0].CanResend {
		t.Errorf("request = %+v", mine[0])
	}
	staff, _ := w.svc.ListForStaff(bg, app)
	if staff[0].Status != StatusDeclined || staff[0].Letter != nil {
		t.Errorf("staff view of a declined request: %+v", staff[0])
	}
	if n := notificationsOf(t, me, "reference_declined"); n != 1 {
		t.Errorf("applicant got %d notifications", n)
	}
	// Организации об отказе рекомендателя не сообщаем: письма нет, сообщать нечего.
	if n := notificationsOf(t, w.tm.Owner, "reference_received") + notificationsOf(t, w.tm.Owner, "reference_declined"); n != 0 {
		t.Errorf("organization got %d notifications", n)
	}
	if err := w.svc.Submit(bg, token, LetterInput{Text: "передумал"}, nil); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("submit after decline: %v", err)
	}
	if err := w.svc.Decline(bg, token); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("second decline: %v", err)
	}
	if err := w.svc.Decline(bg, "нет-такой"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("unknown link: %v", err)
	}
}

func TestResend(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	in := referee()
	req, err := w.svc.Add(bg, me.User, app, in)
	if err != nil {
		t.Fatal(err)
	}
	oldToken := tokenFrom(t, in.Email)

	// Слишком рано.
	_, err = w.svc.Resend(bg, me.User, app, req.ID)
	var soon *TooSoonError
	if !errors.As(err, &soon) || soon.RetryAfter <= 0 || soon.RetryAfter > w.svc.cfg.ResendAfter {
		t.Fatalf("immediate resend: %v", err)
	}
	if len(mailsTo(t, in.Email)) != 1 {
		t.Error("a mail was sent despite the refusal")
	}
	// Ровно через сутки можно.
	w.clock.Advance(w.svc.cfg.ResendAfter - time.Second)
	if _, err := w.svc.Resend(bg, me.User, app, req.ID); !errors.As(err, &soon) {
		t.Fatalf("one second early: %v", err)
	}
	w.clock.Advance(time.Second)
	again, err := w.svc.Resend(bg, me.User, app, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mailsTo(t, in.Email)) != 2 || again.CanResend {
		t.Errorf("mails=%d request=%+v", len(mailsTo(t, in.Email)), again)
	}
	newToken := tokenFrom(t, in.Email)
	if newToken == oldToken {
		t.Fatal("the link did not change")
	}
	if _, err := w.svc.Lookup(bg, oldToken); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("old link still works: %v", err)
	}
	if err := w.svc.Submit(bg, newToken, LetterInput{Text: "ответ"}, nil); err != nil {
		t.Errorf("new link: %v", err)
	}
	// Срок новой ссылки считается от повторной отправки.
	if got := again.ExpiresAt.Sub(w.clock.Now()); got != w.svc.cfg.TTL {
		t.Errorf("new expiry in %v", got)
	}
}

func TestResendAndCancelChecks(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	other := w.Applicant("Другая")
	req, token := w.add(me, app)
	w.clock.Advance(48 * time.Hour)

	// Чужая и несуществующая просьба.
	for name, call := range map[string]func() error{
		"resend by stranger":   func() error { _, err := w.svc.Resend(bg, other.User, app, req.ID); return err },
		"resend by owner":      func() error { _, err := w.svc.Resend(bg, w.tm.Owner.User, app, req.ID); return err },
		"resend unknown":       func() error { _, err := w.svc.Resend(bg, me.User, app, uuid.New()); return err },
		"resend unknown app":   func() error { _, err := w.svc.Resend(bg, me.User, uuid.New(), req.ID); return err },
		"cancel by stranger":   func() error { return w.svc.Cancel(bg, other.User, app, req.ID) },
		"cancel unknown":       func() error { return w.svc.Cancel(bg, me.User, app, uuid.New()) },
		"cancel via wrong app": func() error { return w.svc.Cancel(bg, other.User, w.application(other), req.ID) },
	} {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM reference_requests WHERE id = $1`, req.ID); n != 1 {
		t.Fatal("a refused cancel removed the request")
	}
	// Закрытый отклик: повторять просьбу нельзя.
	w.setStatus(app, "rejected")
	if _, err := w.svc.Resend(bg, me.User, app, req.ID); !errors.Is(err, ErrClosed) {
		t.Errorf("resend for a closed application: %v", err)
	}
	w.setStatus(app, "sent")
	// После ответа повторять и отменять нельзя.
	if err := w.svc.Submit(bg, token, LetterInput{Text: "ответ"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Resend(bg, me.User, app, req.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("resend after answer: %v", err)
	}
	if err := w.svc.Cancel(bg, me.User, app, req.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("cancel after answer: %v", err)
	}
	staff, _ := w.svc.ListForStaff(bg, app)
	if len(staff) != 1 || staff[0].Letter == nil {
		t.Error("the received letter disappeared")
	}
}

func TestCancelKillsTheLink(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	req, token := w.add(me, app)
	if err := w.svc.Cancel(bg, me.User, app, req.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Lookup(bg, token); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("cancelled link still works: %v", err)
	}
	if err := w.svc.Submit(bg, token, LetterInput{Text: "поздно"}, nil); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("cancelled link accepts a letter: %v", err)
	}
	// Освободившееся место и ту же почту можно использовать снова.
	if _, err := w.svc.Add(bg, me.User, app, RefereeInput{Name: "Исправленная Почта", Email: referee().Email}); err != nil {
		t.Error(err)
	}
}

func TestRequestResendFlags(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, _ = w.add(me, app)
	l, _ := w.svc.ListForApplicant(bg, app)
	if l[0].CanResend || l[0].ResendAt == nil {
		t.Errorf("fresh: %+v", l[0])
	}
	w.clock.Advance(w.svc.cfg.ResendAfter)
	l, _ = w.svc.ListForApplicant(bg, app)
	if !l[0].CanResend || l[0].ResendAt != nil {
		t.Errorf("a day later: %+v", l[0])
	}
}

func TestCreateInTxRollsBackWithTheCaller(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	addr := referee()
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	err = w.svc.CreateInTx(bg, dbgen.New(tx), AppInfo{ID: app, ApplicantName: "Мария", VacancyTitle: "V", OrgName: "O"}, []Referee{{Name: "Иван Петров", Email: addr.Email}})
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(bg)
	if n := testkit.Count(t, `SELECT count(*) FROM reference_requests WHERE application_id = $1`, app); n != 0 {
		t.Error("requests survived a rolled back transaction")
	}
	if len(mailsTo(t, addr.Email)) != 0 {
		t.Error("a mail was queued by a rolled back transaction")
	}
}

func TestNewServiceTrimsAddressAndStartsRealClock(t *testing.T) {
	s := NewService(testkit.Pool, notifications.NewService(testkit.Pool, notifications.Config{}), DefaultConfig("SciBox", "http://localhost:5173///"))
	if s.cfg.PublicURL != "http://localhost:5173" || time.Since(s.now()) > time.Minute {
		t.Errorf("url=%q now=%v", s.cfg.PublicURL, s.now())
	}
}

// Между проверкой ссылки и записью ответа ссылку успели использовать: ответ не записывается второй раз.
func TestAnswerLosesTheRaceForTheLink(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, token := w.add(me, app)
	db, _ := testkit.Faulty(testkit.Pool, 0)
	once := sync.Once{}
	db.Hook = func(sql string) {
		if strings.Contains(sql, "UPDATE reference_requests SET status") {
			once.Do(func() {
				if _, err := testkit.Pool.Exec(bg, `UPDATE reference_requests SET status = 'declined', answered_at = now() WHERE application_id = $1`, app); err != nil {
					t.Error(err)
				}
			})
		}
	}
	err := svcOn(w, db).Submit(bg, token, LetterInput{Text: "поздно"}, &files.Upload{Name: "a.pdf", Data: pdfBytes()})
	if !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("err = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1`, app); n != 0 {
		t.Errorf("%d files saved by the loser", n)
	}
	if n := notificationsOf(t, w.tm.Owner, "reference_received"); n != 0 {
		t.Error("the organization was told about a letter that was not saved")
	}
}
