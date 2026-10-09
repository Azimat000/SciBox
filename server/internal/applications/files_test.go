package applications

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/auth"
	"scibox/server/internal/files"
	"scibox/server/internal/testkit"
)

func fileURL(app, file uuid.UUID) string {
	return "/api/applications/" + app.String() + "/files/" + file.String()
}

// Заражённый файл отклоняется до отклика: нет ни отклика, ни следов в хранилище, а человек получает понятное сообщение.
func TestHTTPApplyWithAVirusIsRefused(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	in := a.input(me)
	objects := a.Store.Count()
	eicar := append(append([]byte("%PDF-1.7\n"), antivirus.EICAR()...), '\n')

	r := a.form(&me, in, testkit.FilePart{Name: "ok.pdf", Data: pdfBytes()}, testkit.FilePart{Name: "счёт.pdf", Data: eicar})
	msg, _ := r.Fields(t)["files"].(string)
	if r.Code != 422 || !strings.Contains(msg, "«счёт.pdf»") || !strings.Contains(msg, "вирус") || strings.Contains(r.Raw, "Eicar") {
		t.Fatalf("answer: %d %s", r.Code, r.Raw)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID); n != 0 {
		t.Error("an application with a virus was created")
	}
	if a.Store.Count() != objects {
		t.Errorf("files stayed in the storage after the refusal: %v", a.Store.Keys())
	}
	// Тот же отклик без вируса проходит: человек исправил файл.
	if r := a.form(&me, in, testkit.FilePart{Name: "ok.pdf", Data: pdfBytes()}); r.Code != 201 {
		t.Errorf("after the fix: %d %s", r.Code, r.Raw)
	}
}

// Запрос с неверными полями не доходит до хранилища и антивируса.
func TestHTTPApplyWithWrongFieldsNeverTouchesTheStorage(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	scans := a.AV.Scans()
	r := a.form(&me, Input{VacancyID: a.vacancy.ID, ContactEmail: "x", CoverLetter: "коротко"}, testkit.FilePart{Name: "a.pdf", Data: pdfBytes()})
	if r.Code != 422 || r.Fields(t)["contact_email"] == nil {
		t.Fatalf("answer: %d %s", r.Code, r.Raw)
	}
	if a.Store.Count() != 0 || a.AV.Scans() != scans {
		t.Errorf("objects %d, scans %d", a.Store.Count(), a.AV.Scans()-scans)
	}
}

// Антивирус недоступен: отклик принят, файл ждёт проверки и никому не выдаётся, пока не проверен.
func TestAFileWaitingForTheCheckIsNotServedToAnyone(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	a.AV.Fault = func() error { return antivirus.ErrUnavailable }
	r := a.form(&me, a.input(me), testkit.FilePart{Name: "Список.pdf", Data: pdfBytes()})
	if r.Code != 201 {
		t.Fatalf("apply while the antivirus is down: %d %s", r.Code, r.Raw)
	}
	app := r.JSON(t)["application"].(map[string]any)
	id := uuid.MustParse(app["id"].(string))
	f := app["files"].([]any)[0].(map[string]any)
	fid := uuid.MustParse(f["id"].(string))
	if f["scan"] != "pending" || app["cv"].(map[string]any)["scan"] != "clean" {
		t.Fatalf("scan states: file %v, cv %v", f["scan"], app["cv"])
	}

	downloads := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE action = 'application.file_downloaded'`)
	for name, who := range map[string]*testkit.Person{"applicant": &me, "owner": &a.tm.Owner, "hr": &a.tm.HR} {
		r := a.api.Do(who, "GET", fileURL(id, fid), nil)
		if r.Code != 409 || r.ErrCode(t) != CodeFilePending || strings.Contains(r.Raw, "%PDF") {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE action = 'application.file_downloaded'`); n != downloads {
		t.Errorf("a refused download was written to the journal: %d", n-downloads)
	}
	// Организация видит, что файл есть и проверяется; резюме, собранное сервером, доступно сразу.
	staff := a.api.Do(&a.tm.Owner, "GET", "/api/applications/"+id.String(), nil).JSON(t)["application"].(map[string]any)
	if got := staff["files"].([]any)[0].(map[string]any)["scan"]; got != "pending" {
		t.Errorf("the organization sees scan = %v", got)
	}
	if r := a.api.Do(&a.tm.Owner, "GET", fileURL(id, uuid.MustParse(staff["cv"].(map[string]any)["id"].(string))), nil); r.Code != 200 {
		t.Errorf("cv: %d", r.Code)
	}

	// Проверка прошла (так делает фоновая задача): файл доступен.
	if _, err := testkit.Pool.Exec(bg, `UPDATE application_files SET scan_status = 'clean', scanned_at = now() WHERE id = $1`, fid); err != nil {
		t.Fatal(err)
	}
	if r := a.api.Do(&a.tm.Owner, "GET", fileURL(id, fid), nil); r.Code != 200 || !bytes.HasPrefix([]byte(r.Raw), []byte("%PDF-")) {
		t.Errorf("after the check: %d", r.Code)
	}
}

// Заражённый файл (найденный позже) организации не виден вовсе, автору виден как отвергнутый, скачать его нельзя никому.
func TestAnInfectedFileIsHiddenFromTheOrganizationAndNeverServed(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	d := a.mustApply(me, nil, a.pdf("bad.pdf"), a.pdf("good.pdf"))
	var badID uuid.UUID
	for _, f := range d.Files {
		if f.Name == "bad.pdf" {
			badID = f.ID
		}
	}
	var key string
	if err := testkit.Pool.QueryRow(bg, `SELECT storage_key FROM application_files WHERE id = $1`, badID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := testkit.Pool.Exec(bg, `UPDATE application_files SET scan_status = 'infected', scan_signature = 'Test', storage_key = NULL WHERE id = $1`, badID); err != nil {
		t.Fatal(err)
	}
	_ = a.Store.Delete(bg, key)

	mine, err := a.svc.Get(bg, me.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	scans := map[string]string{}
	for _, f := range mine.Files {
		scans[f.Name] = f.Scan
	}
	if scans["bad.pdf"] != "infected" || scans["good.pdf"] != "clean" {
		t.Errorf("the applicant sees %v", scans)
	}
	staff, err := a.svc.Get(bg, a.tm.Owner.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(staff.Files) != 1 || staff.Files[0].Name != "good.pdf" {
		t.Errorf("the organization sees %+v", staff.Files)
	}
	for name, who := range map[string]testkit.Person{"applicant": me, "owner": a.tm.Owner} {
		if _, _, err := readFile(bg, a.svc, who.User, d.ID, badID); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s downloads an infected file: %v", name, err)
		}
	}
}

func TestStorageFailuresAreNotNotFoundAndLeaveNoTrace(t *testing.T) {
	t.Run("apply while the storage is down", func(t *testing.T) {
		a := newAPI(t)
		me := a.Applicant("Мария")
		a.Store.Fault = func(op, _ string) error {
			if op == "put" {
				return errors.New("storage down")
			}
			return nil
		}
		r := a.form(&me, a.input(me), testkit.FilePart{Name: "a.pdf", Data: pdfBytes()})
		if r.Code != 503 || r.ErrCode(t) != "storage_unavailable" || strings.Contains(r.Raw, "storage down") {
			t.Fatalf("answer: %d %s", r.Code, r.Raw)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID); n != 0 {
			t.Error("an application was created without its files")
		}
		a.Store.Fault = nil
		if r := a.form(&me, a.input(me), testkit.FilePart{Name: "a.pdf", Data: pdfBytes()}); r.Code != 201 {
			t.Errorf("after recovery: %d %s", r.Code, r.Raw)
		}
	})
	t.Run("apply without attachments while the storage is down (the cv is stored by the service)", func(t *testing.T) {
		w := newWorld(t)
		me := w.Applicant("Мария")
		w.Store.Fault = func(op, _ string) error {
			if op == "put" {
				return errors.New("storage down")
			}
			return nil
		}
		_, err := w.apply(me, nil)
		if !errors.Is(err, files.ErrStorage) {
			t.Fatalf("err = %v", err)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID); n != 0 {
			t.Error("an application was created without its cv")
		}
	})
	t.Run("download while the storage is down", func(t *testing.T) {
		a := newAPI(t)
		me := a.Applicant("Мария")
		d := a.mustApply(me, nil)
		a.Store.Fault = func(op, _ string) error {
			if op == "get" {
				return errors.New("storage down")
			}
			return nil
		}
		downloads := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE action = 'application.file_downloaded'`)
		r := a.api.Do(&a.tm.Owner, "GET", fileURL(d.ID, d.CV.ID), nil)
		if r.Code != 503 || r.ErrCode(t) != "storage_unavailable" || strings.Contains(r.Raw, "storage down") {
			t.Fatalf("answer: %d %s", r.Code, r.Raw)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM audit_log WHERE action = 'application.file_downloaded'`); n != downloads {
			t.Error("a download that did not happen was written to the journal")
		}
	})
	t.Run("a clean file whose object is gone is an error, not a quiet 404", func(t *testing.T) {
		a := newAPI(t)
		me := a.Applicant("Мария")
		d := a.mustApply(me, nil)
		for _, k := range a.Store.Keys() {
			_ = a.Store.Delete(bg, k)
		}
		r := a.api.Do(&a.tm.Owner, "GET", fileURL(d.ID, d.CV.ID), nil)
		if r.Code != 500 || r.ErrCode(t) != "internal" {
			t.Fatalf("answer: %d %s", r.Code, r.Raw)
		}
	})
	t.Run("the journal is down: no file is given out", func(t *testing.T) {
		a := newAPI(t)
		me := a.Applicant("Мария")
		d := a.mustApply(me, nil)
		_, err := testkit.Pool.Exec(bg, `ALTER TABLE audit_log RENAME TO audit_log_off`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = testkit.Pool.Exec(bg, `ALTER TABLE audit_log_off RENAME TO audit_log`) }()
		r := a.api.Do(&a.tm.Owner, "GET", fileURL(d.ID, d.CV.ID), nil)
		if r.Code != 500 || strings.Contains(r.Raw, "%PDF") {
			t.Errorf("answer: %d", r.Code)
		}
	})
}

// Отказ, который случился до записи в базу, забирает файлы; сбой базы файлы не трогает (их судьбу решит чистка).
func TestRefusalsTakeTheFilesBack(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	w.mustApply(me, nil)
	before := w.Store.Count()
	// Повторный отклик на ту же вакансию: отказ внутри транзакции.
	_, err := w.apply(me, nil, w.pdf("again.pdf"))
	if !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("err = %v", err)
	}
	if w.Store.Count() != before {
		t.Errorf("objects: %d, want %d (the cv and the attachment of a refused application must go)", w.Store.Count(), before)
	}
	// Вакансия закрыта.
	other := w.Applicant("Пётр")
	closed := w.tm.Published(&w.tm.UnitA.ID)
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET status = 'closed' WHERE id = $1`, closed.ID); err != nil {
		t.Fatal(err)
	}
	before = w.Store.Count()
	in := w.input(other)
	in.VacancyID = closed.ID
	if _, err := w.svc.Apply(bg, other.User, in, []files.Stored{w.pdf("x.pdf")}); !errors.Is(err, ErrVacancyClosed) {
		t.Fatalf("err = %v", err)
	}
	if w.Store.Count() != before {
		t.Errorf("objects after a closed vacancy: %d, want %d", w.Store.Count(), before)
	}
}

func TestKnownRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"validation":        {&auth.ValidationError{Fields: map[string]string{"x": "y"}}, true},
		"rate limit":        {&auth.RateLimitedError{}, true},
		"wrapped not found": {fmt.Errorf("x: %w", ErrNotFound), true},
		"closed":            {ErrVacancyClosed, true},
		"deadline":          {ErrDeadlinePassed, true},
		"own vacancy":       {ErrOwnVacancy, true},
		"already applied":   {ErrAlreadyApplied, true},
		// Эти нельзя считать отказом: транзакция могла пройти.
		"database failure":  {testkit.ErrFault, false},
		"commit failure":    {errors.New("applications: commit: connection reset"), false},
		"storage failure":   {files.ErrStorage, false},
		"bad status change": {ErrBadStatus, false},
		"nil":               {nil, false},
	} {
		if got := knownRefusal(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestDownloadStreamsTheWholeFile(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	big := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("0123456789abcdef"), 1<<16)...) // 1 МБ
	d := a.mustApply(me, nil, a.Stored("Большой.pdf", big))
	r := a.api.Do(&me, "GET", fileURL(d.ID, d.Files[0].ID), nil)
	if r.Code != 200 || r.Header.Get("Content-Length") != fmt.Sprint(len(big)) || r.Raw != string(big) {
		t.Fatalf("code %d, length %q, body %d bytes", r.Code, r.Header.Get("Content-Length"), len(r.Raw))
	}
}

func TestCheckInput(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	if err := w.svc.CheckInput(me.User, w.input(me)); err != nil {
		t.Errorf("a good application: %v", err)
	}
	var v *auth.ValidationError
	if err := w.svc.CheckInput(me.User, Input{ContactEmail: "x"}); !errors.As(err, &v) || v.Fields["contact_email"] == "" || v.Fields["cover_letter"] == "" {
		t.Errorf("a bad application: %v", err)
	}
}
