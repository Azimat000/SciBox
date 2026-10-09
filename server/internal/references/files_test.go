package references

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
)

func eicarPDF() []byte {
	return append(append([]byte("%PDF-1.7\n"), antivirus.EICAR()...), '\n')
}

func submit(a *apiWorld, token, text string, parts ...testkit.FilePart) testkit.Reply {
	return a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token, "text": text}, parts...)
}

// Письмо с вирусом отклоняется понятным сообщением, ссылка остаётся живой, в хранилище ничего не остаётся.
func TestHTTPLetterWithAVirusIsRefusedAndTheLinkSurvives(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	app := a.application(me)
	_, token := a.add(me, app)
	objects := a.Store.Count()

	r := submit(a, token, "Рекомендую", testkit.FilePart{Name: "Письмо.pdf", Data: eicarPDF()})
	msg, _ := r.Fields(t)["file"].(string)
	if r.Code != 422 || !strings.Contains(msg, "вирус") || !strings.Contains(msg, "«Письмо.pdf»") || strings.Contains(r.Raw, "Eicar") {
		t.Fatalf("answer: %d %s", r.Code, r.Raw)
	}
	if a.Store.Count() != objects {
		t.Errorf("the infected letter stayed in the storage: %v", a.Store.Keys())
	}
	if r := submit(a, token, "Рекомендую", testkit.FilePart{Name: "Письмо.pdf", Data: pdfBytes()}); r.Code != 200 {
		t.Errorf("the link did not survive: %d %s", r.Code, r.Raw)
	}
}

// Мёртвая ссылка и слишком длинный текст не доходят до хранилища и антивируса: их видно из первой части запроса.
func TestHTTPLetterIsCheckedBeforeTheFileIsTaken(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	app := a.application(me)
	_, token := a.add(me, app)
	file := testkit.FilePart{Name: "a.pdf", Data: pdfBytes()}
	scans := a.AV.Scans()

	if r := submit(a, "не-ссылка", "x", file); r.Code != 404 || r.ErrCode(t) != CodeInvalidLink {
		t.Errorf("unknown link: %d %s", r.Code, r.Raw)
	}
	if r := submit(a, token, strings.Repeat("я", MaxLetterRunes+1), file); r.Code != 422 || r.Fields(t)["text"] == nil {
		t.Errorf("long text: %d %s", r.Code, r.Raw)
	}
	a.setStatus(app, "withdrawn")
	if r := submit(a, token, "x", file); r.Code != 410 {
		t.Errorf("withdrawn application: %d %s", r.Code, r.Raw)
	}
	if a.Store.Count() != 0 || a.AV.Scans() != scans {
		t.Errorf("a request refused by its first part reached the storage (%d objects) or the antivirus (%d scans)", a.Store.Count(), a.AV.Scans()-scans)
	}
}

func TestHTTPLetterWhileTheStorageIsDownKeepsTheLink(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	_, token := a.add(me, a.application(me))
	a.Store.Fault = func(op, _ string) error {
		if op == "put" {
			return errors.New("storage down")
		}
		return nil
	}
	r := submit(a, token, "Рекомендую", testkit.FilePart{Name: "a.pdf", Data: pdfBytes()})
	if r.Code != 503 || r.ErrCode(t) != "storage_unavailable" || strings.Contains(r.Raw, "storage down") {
		t.Fatalf("answer: %d %s", r.Code, r.Raw)
	}
	a.Store.Fault = nil
	if r := submit(a, token, "Рекомендую", testkit.FilePart{Name: "a.pdf", Data: pdfBytes()}); r.Code != 200 {
		t.Errorf("after recovery: %d %s", r.Code, r.Raw)
	}
}

// Отказ сервиса (ссылка использована, истекла) забирает файл; антивирус недоступен — письмо ждёт проверки и организации
// показано как «проверяется»; заражённого письма организация не видит.
func TestLetterFileStatesForTheOrganization(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)

	t.Run("a refusal takes the file back", func(t *testing.T) {
		_, token := w.add(me, app)
		if err := w.svc.Submit(bg, token, LetterInput{Text: "первое"}, nil); err != nil {
			t.Fatal(err)
		}
		up := w.pdf("late.pdf")
		if err := w.svc.Submit(bg, token, LetterInput{Text: "второе"}, &up); !errors.Is(err, ErrAlreadyAnswered) {
			t.Fatalf("err = %v", err)
		}
		if w.Store.Has(up.Key) {
			t.Error("the file of a refused letter stayed in the storage")
		}
	})

	t.Run("waiting and infected letters", func(t *testing.T) {
		other := w.Applicant("Вторая")
		app2 := w.application(other)
		_, token := w.add(other, app2)
		w.AV.Fault = func() error { return antivirus.ErrUnavailable }
		up := w.pdf("wait.pdf")
		w.AV.Fault = nil
		if up.Scan != "pending" {
			t.Fatalf("the antivirus was down, yet the file is %q", up.Scan)
		}
		if err := w.svc.Submit(bg, token, LetterInput{}, &up); err != nil {
			t.Fatal(err)
		}
		staff, err := w.svc.ListForStaff(bg, app2)
		if err != nil || len(staff) != 1 || staff[0].Letter == nil || staff[0].Letter.File == nil || staff[0].Letter.File.Scan != "pending" {
			t.Fatalf("staff view: %+v %v", staff, err)
		}
		if _, err := testkit.Pool.Exec(bg, `UPDATE application_files SET scan_status = 'infected', scan_signature = 'Test', storage_key = NULL WHERE application_id = $1`, app2); err != nil {
			t.Fatal(err)
		}
		staff, err = w.svc.ListForStaff(bg, app2)
		if err != nil || len(staff) != 1 || staff[0].Letter == nil || staff[0].Letter.File != nil {
			t.Fatalf("an infected letter file is visible to the organization: %+v %v", staff, err)
		}
	})
}

func TestCheckSubmit(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	app := w.application(me)
	_, token := w.add(me, app)
	var verr *auth.ValidationError

	if err := w.svc.CheckSubmit(bg, token, LetterInput{Text: "ok"}); err != nil {
		t.Errorf("a live link: %v", err)
	}
	if err := w.svc.CheckSubmit(bg, "нет", LetterInput{}); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("unknown link: %v", err)
	}
	if err := w.svc.CheckSubmit(bg, token, LetterInput{Text: strings.Repeat("я", MaxLetterRunes+1)}); !errors.As(err, &verr) || verr.Fields["text"] == "" {
		t.Errorf("long text: %v", err)
	}
	if err := w.svc.CheckSubmit(bg, token, LetterInput{Text: " " + strings.Repeat("я", MaxLetterRunes) + " "}); err != nil {
		t.Errorf("text of the maximum length with spaces around: %v", err)
	}
	w.setStatus(app, "withdrawn")
	if err := w.svc.CheckSubmit(bg, token, LetterInput{}); !errors.Is(err, ErrGone) {
		t.Errorf("withdrawn: %v", err)
	}
	w.setStatus(app, "sent")
	if err := w.svc.Submit(bg, token, LetterInput{Text: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CheckSubmit(bg, token, LetterInput{}); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("answered: %v", err)
	}
}

func TestKnownRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"validation":      {&auth.ValidationError{}, true},
		"invalid link":    {fmt.Errorf("x: %w", ErrInvalidLink), true},
		"already":         {ErrAlreadyAnswered, true},
		"gone":            {ErrGone, true},
		"expired":         {ErrExpired, true},
		"database":        {testkit.ErrFault, false},
		"commit":          {errors.New("references: commit: reset"), false},
		"not pending":     {ErrNotPending, false},
		"nil":             {nil, false},
		"storage failure": {errors.New("storage"), false},
	} {
		if got := knownRefusal(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestVaultAccessor(t *testing.T) {
	w := newWorld(t)
	if w.svc.Vault() != w.Vault {
		t.Error("Vault() returns another vault")
	}
}
