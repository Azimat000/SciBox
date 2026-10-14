package applications

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/testaudit"
	"scibox/server/internal/testkit"
)

func exportIDs(e Export) []uuid.UUID {
	out := make([]uuid.UUID, len(e.Rows))
	for i, r := range e.Rows {
		out[i] = r.ID
	}
	return out
}

// Выгрузка показывает ровно то же множество откликов, что и список откликов: кто что вправе разбирать, решает
// access.ViewApplications. Таблица «кто → сколько откликов» та же, что у списка.
func TestExportFollowsRightsOfTheList(t *testing.T) {
	l := newListWorld(t)
	for _, r := range []struct {
		name string
		who  testkit.Person
		want int
	}{
		{"owner", l.tm.Owner, 4}, {"hr", l.tm.HR, 4}, {"head A", l.tm.HeadA, 3}, {"head B", l.tm.HeadB, 1},
		{"outsider", l.tm.Out, 0}, {"applicant", l.applics[0], 0}, {"owner of another organization", l.other.Owner, 1},
	} {
		list, err := l.svc.Candidates(bg, r.who.User, CandidateFilter{})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		got, err := l.svc.ExportCandidates(bg, r.who.User, CandidateFilter{})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if len(got.Rows) != r.want || !slices.Equal(exportIDs(got), ids(list)) || got.Truncated {
			t.Errorf("%s: export %v, list %v, want %d rows", r.name, exportIDs(got), ids(list), r.want)
		}
	}
	// Чужая вакансия в отборе даёт пустой файл, а не ошибку и не чужие отклики.
	got, err := l.svc.ExportCandidates(bg, l.tm.Owner.User, CandidateFilter{VacancyID: l.vacOth.ID})
	if err != nil || len(got.Rows) != 0 {
		t.Errorf("foreign vacancy: %+v %v", got, err)
	}
}

func TestExportFilters(t *testing.T) {
	l := newListWorld(t)
	owner := l.tm.Owner.User
	l.setStatus(l.appA[0].ID, StatusInvited)
	l.setStatus(l.appA[1].ID, StatusWithdrawn)
	for name, f := range map[string]CandidateFilter{
		"vacancy": {VacancyID: l.vacA.ID}, "status": {Status: StatusInvited}, "withdrawn are listed too": {Status: StatusWithdrawn},
		"vacancy and status": {Status: StatusSent, VacancyID: l.vacA.ID}, "paging is ignored": {Limit: 1, Offset: 2},
	} {
		list, _ := l.svc.Candidates(bg, owner, CandidateFilter{VacancyID: f.VacancyID, Status: f.Status, Limit: 50})
		got, err := l.svc.ExportCandidates(bg, owner, f)
		if err != nil || !slices.Equal(exportIDs(got), ids(list)) {
			t.Errorf("%s: export %v, list %v (%v)", name, exportIDs(got), ids(list), err)
		}
	}
	var verr *auth.ValidationError
	if _, err := l.svc.ExportCandidates(bg, owner, CandidateFilter{Status: "done"}); !errors.As(err, &verr) || verr.Fields["status"] == "" {
		t.Errorf("unknown status: %v", err)
	}
}

func TestExportIsCappedAndSaysSo(t *testing.T) {
	l := newListWorld(t)
	defer func(old int) { exportLimit = old }(exportLimit)
	exportLimit = 3
	got, err := l.svc.ExportCandidates(bg, l.tm.Owner.User, CandidateFilter{})
	if err != nil || len(got.Rows) != 3 || !got.Truncated {
		t.Fatalf("%d rows, truncated %v, %v", len(got.Rows), got.Truncated, err)
	}
	exportLimit = 4 // ровно по пределу: «не поместилось» нет
	if got, _ := l.svc.ExportCandidates(bg, l.tm.Owner.User, CandidateFilter{}); len(got.Rows) != 4 || got.Truncated {
		t.Errorf("%d rows, truncated %v", len(got.Rows), got.Truncated)
	}
}

// Что попадает в строку: данные самого отклика и снимка профиля на момент отправки.
func TestExportRowContent(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария Петрова")
	d := w.mustApply(me, func(i *Input) { i.ContactEmail = "maria.contact@example.ru" })
	if _, err := testkit.Pool.Exec(bg, `
		UPDATE applications SET profile = jsonb_set(jsonb_set(jsonb_set(profile,
			'{degree,level}', '"doctor"'), '{academic_title}', '"professor"'),
			'{h_index}', '{"rsci": 7, "scopus": 21, "wos": null, "scholar": 14}') WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.ExportCandidates(bg, w.tm.Owner.User, CandidateFilter{})
	if err != nil || len(got.Rows) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	r := got.Rows[0]
	if r.ID != d.ID || r.ApplicantName != "Мария Петрова" || r.ContactEmail != "maria.contact@example.ru" ||
		r.VacancyTitle != w.vacancy.Title || r.OrgName == "" || r.UnitName == "" || r.Status != StatusSent {
		t.Errorf("row: %+v", r)
	}
	if r.Degree != "doctor" || r.AcademicTitle != "professor" || r.Level != 4 || r.HIndex == nil || *r.HIndex != 21 {
		t.Errorf("snapshot: %+v", r)
	}
	// Профиль, каким он стал после отклика, в выгрузку не попадает: берётся снимок.
	if _, err := testkit.Pool.Exec(bg, `UPDATE profiles SET h_scopus = 99, degree = 'none' WHERE user_id = $1`, me.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := w.svc.ExportCandidates(bg, w.tm.Owner.User, CandidateFilter{})
	if again.Rows[0].HIndex == nil || *again.Rows[0].HIndex != 21 || again.Rows[0].Degree != "doctor" {
		t.Errorf("the export must use the snapshot: %+v", again.Rows[0])
	}
}

// Обезличенный отклик (аккаунт удалён) и повреждённый снимок не ломают выгрузку: остаются только имя из аккаунта и
// сведения об отклике, степень, уровень и контакты пусты.
func TestExportOfAnonymizedAndBrokenSnapshots(t *testing.T) {
	w := newWorld(t)
	gone := w.mustApply(w.Applicant("Борис Удалённый"), nil)
	broken := w.mustApply(w.Applicant("Вера Повреждённая"), nil)
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET anonymized_at = now(), contact_email = '', cover_letter = '', profile = '{}'::jsonb WHERE id = $1`, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET profile = '"not an object"'::jsonb WHERE id = $1`, broken.ID); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.ExportCandidates(bg, w.tm.Owner.User, CandidateFilter{})
	if err != nil || len(got.Rows) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, r := range got.Rows {
		if r.ApplicantName == "" || r.VacancyTitle == "" {
			t.Errorf("the application itself must stay in the file: %+v", r)
		}
		if r.Degree != "" || r.AcademicTitle != "" || r.Level != 0 || r.HIndex != nil {
			t.Errorf("no snapshot, no science data: %+v", r)
		}
		if r.ID == gone.ID && r.ContactEmail != "" {
			t.Errorf("contacts of an erased account: %q", r.ContactEmail)
		}
	}
}

// Выгрузка пишется в журнал организации (по записи на организацию), пустая не пишется, отказ не пишет ничего.
func TestExportIsAudited(t *testing.T) {
	l := newListWorld(t)
	ctx := audit.WithSource(bg, "203.0.113.77")
	rows := func(org uuid.UUID) []testaudit.Row {
		return testaudit.Rows(t, testkit.Pool, `org_id = $1 AND action = 'vacancy.applications_exported'`, org)
	}
	if _, err := l.svc.ExportCandidates(ctx, l.tm.Owner.User, CandidateFilter{}); err != nil {
		t.Fatal(err)
	}
	got := rows(l.tm.OrgID)
	if len(got) != 1 || got[0].Actor == nil || *got[0].Actor != l.tm.Owner.ID || got[0].Details["rows"] != "4" ||
		got[0].Details["scope"] != "list" || got[0].TargetKind != "" || got[0].IP == nil || *got[0].IP != "203.0.113.77" {
		t.Fatalf("whole list: %+v", got)
	}
	if _, err := l.svc.ExportCandidates(ctx, l.tm.HeadA.User, CandidateFilter{VacancyID: l.vacA.ID, Status: StatusSent}); err != nil {
		t.Fatal(err)
	}
	got = rows(l.tm.OrgID)
	last := got[len(got)-1]
	if len(got) != 2 || last.TargetKind != "vacancy" || last.TargetID == nil || *last.TargetID != l.vacA.ID || last.TargetLabel != l.vacA.Title ||
		last.Details["rows"] != "3" || last.Details["scope"] != "vacancy" || last.Details["status"] != StatusSent {
		t.Errorf("one vacancy: %+v", got)
	}
	// Люди в записи не названы: ни имён, ни почт.
	for _, r := range got {
		for k, v := range r.Details {
			if strings.Contains(v, "@") || strings.Contains(k, "name") {
				t.Errorf("personal data in the audit record: %v", r.Details)
			}
		}
	}
	// Пустая выгрузка, отказ и чужая организация не оставляют записей.
	before := len(rows(l.tm.OrgID))
	for _, who := range []testkit.Person{l.tm.Out, l.applics[0]} {
		if got, err := l.svc.ExportCandidates(ctx, who.User, CandidateFilter{}); err != nil || len(got.Rows) != 0 {
			t.Errorf("%+v %v", got, err)
		}
	}
	if _, err := l.svc.ExportCandidates(ctx, l.tm.Owner.User, CandidateFilter{VacancyID: l.vacOth.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.svc.ExportCandidates(ctx, l.tm.Owner.User, CandidateFilter{Status: "done"}); err == nil {
		t.Fatal("unknown status")
	}
	if len(rows(l.tm.OrgID)) != before || len(rows(l.other.OrgID)) != 0 {
		t.Errorf("an empty or refused export left records: %d → %d, other %d", before, len(rows(l.tm.OrgID)), len(rows(l.other.OrgID)))
	}
	// Организация видит запись о выгрузке в своём журнале; у другой организации её нет.
	if _, err := l.svc.ExportCandidates(ctx, l.other.Owner.User, CandidateFilter{}); err != nil {
		t.Fatal(err)
	}
	if n := len(rows(l.other.OrgID)); n != 1 {
		t.Errorf("other organization: %d records", n)
	}
}

// Сбой журнала отменяет выгрузку: нет записи — нет файла.
func TestExportWithoutAuditIsRefused(t *testing.T) {
	l := newListWorld(t)
	testaudit.Break(t, testkit.Pool)
	if got, err := l.svc.ExportCandidates(bg, l.tm.Owner.User, CandidateFilter{}); err == nil || len(got.Rows) != 0 {
		t.Errorf("export without the audit record: %+v %v", got, err)
	}
}

func TestWriteCSV(t *testing.T) {
	at := time.Date(2026, 10, 14, 6, 30, 0, 0, time.UTC) // 09:30 по Москве
	id := uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")
	hi := 14
	rows := []ExportRow{{
		ID: id, ApplicantName: "Мария Петрова", ContactEmail: "maria@example.ru", VacancyTitle: "Научный сотрудник; лаборатория «Катализ»",
		OrgName: "Институт", UnitName: "Лаборатория А", Status: StatusInvited, CreatedAt: at, StatusChangedAt: at.Add(26 * time.Hour),
		Degree: "candidate", AcademicTitle: "docent", Level: 2, HIndex: &hi,
	}, {
		ID: uuid.New(), ApplicantName: "Борис", Status: StatusWithdrawn, CreatedAt: at, StatusChangedAt: at, VacancyTitle: "Вакансия",
	}}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, rows, "https://scibox.example.ru/"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\uFEFF") {
		t.Error("нет метки BOM: Excel покажет «кракозябры»")
	}
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(out, "\uFEFF"), "\r\n"), "\r\n")
	if len(lines) != 3 {
		t.Fatalf("%d строк:\n%s", len(lines), out)
	}
	if lines[0] != strings.Join(exportHeader, ";") {
		t.Errorf("заголовок: %q", lines[0])
	}
	want := `Мария Петрова;maria@example.ru;"Научный сотрудник; лаборатория «Катализ»";Институт;Лаборатория А;Приглашён;` +
		`14.10.2026 09:30;15.10.2026 11:30;Кандидат наук;Доцент;R2;14;https://scibox.example.ru/candidates/` + id.String()
	if lines[1] != want {
		t.Errorf("строка:\n%s\nожидали\n%s", lines[1], want)
	}
	// Отзванный отклик без снимка: пустые степень, звание, уровень и h-index; ячейки на месте.
	if cols := strings.Split(lines[2], ";"); len(cols) != len(exportHeader) || cols[5] != "Отозван" || cols[8] != "" || cols[10] != "" || cols[11] != "" {
		t.Errorf("пустые столбцы: %q", lines[2])
	}
}

// Формула в имени или названии не должна выполниться в Excel (T-87).
func TestCSVNeutralizesFormulas(t *testing.T) {
	for in, want := range map[string]string{
		"=HYPERLINK(\"http://x\")": "'=HYPERLINK(\"http://x\")", "+7 999 000-00-00": "'+7 999 000-00-00", "-1+1": "'-1+1", "@SUM(A1)": "'@SUM(A1)",
		"\tтаб": "'\tтаб", "\rвозврат": "'\rвозврат", "Анна": "Анна", "": "", "a=b": "a=b", "1-2": "1-2",
	} {
		if got := cell(in); got != want {
			t.Errorf("cell(%q) = %q, want %q", in, got, want)
		}
	}
	var buf bytes.Buffer
	row := ExportRow{ID: uuid.New(), ApplicantName: "=cmd|' /C calc'!A0", ContactEmail: "-x@example.ru", VacancyTitle: "@1", OrgName: "+2", UnitName: "ok", CreatedAt: time.Now(), StatusChangedAt: time.Now()}
	if err := WriteCSV(&buf, []ExportRow{row}, ""); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, bad := range []string{";=cmd", "\r\n=cmd", ";-x@", ";@1;", ";+2;"} {
		if strings.Contains(body, bad) {
			t.Errorf("в файле осталась формула %q:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, "'=cmd") || !strings.Contains(body, "'-x@example.ru") {
		t.Errorf("апостроф не поставлен:\n%s", body)
	}
}
