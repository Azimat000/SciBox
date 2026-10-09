package dataexport_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/dataexport"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/references"
	"scibox/server/internal/testaudit"
	"scibox/server/internal/testkit"
)

var bg = testkit.BG

type world struct {
	*testkit.World
	apps *applications.Service
	tm   *testkit.Team
	vac  uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	notes := notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	refs := references.NewService(testkit.Pool, notes, k.Vault, references.DefaultConfig("SciBox", "http://localhost:5173"))
	w := &world{World: k, apps: applications.NewService(testkit.Pool, k.Prof, refs, notes, k.Vault, applications.Config{Apply: applications.Limit{Max: 1000, Window: time.Hour}})}
	w.tm = k.Team()
	w.vac = w.tm.Published(&w.tm.UnitA.ID).ID
	return w
}

// pdfData — PDF с меткой marker внутри.
func pdfData(marker string) []byte {
	return append([]byte("%PDF-1.7\n% "+marker+"\n"), make([]byte, 300)...)
}

// pdf кладёт на склад PDF с меткой marker внутри.
func (w *world) pdf(name, marker string) files.Stored {
	w.T.Helper()
	return w.Stored(name, pdfData(marker))
}

func (w *world) apply(who testkit.Person, cover, refereeEmail string, ups ...files.Stored) applications.Detail {
	w.T.Helper()
	d, err := w.apps.Apply(bg, who.User, applications.Input{
		VacancyID: w.vac, ContactEmail: who.Email, CoverLetter: cover,
		Referees: []references.RefereeInput{{Name: "Профессор " + refereeEmail, Email: refereeEmail, Relation: "научный руководитель"}},
	}, ups)
	if err != nil {
		w.T.Fatalf("apply: %v", err)
	}
	return d
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testkit.Pool.Exec(bg, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// fresh входит по паролю: такой сеанс «свежий» (D-144).
func fresh(t *testing.T, w *testkit.World, email string) auth.Principal {
	t.Helper()
	_, s, err := w.Auth.Login(bg, email, testkit.Password, testkit.Meta())
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.Auth.Authenticate(bg, s.Token)
	if err != nil || !p.Fresh {
		t.Fatalf("fresh session: %+v %v", p, err)
	}
	return p
}

// built — человек с полными данными и посторонний с другими; всё, что должно и не должно попасть в архив.
type built struct {
	me, other      testkit.Person
	attachment     files.Stored
	attachmentData []byte
	meApp, otherID uuid.UUID
}

const (
	myCover       = "МОЁ-ПИСЬМО: изучаю активные центры катализаторов"
	otherCover    = "ЧУЖОЕ-ПИСЬМО: занимаюсь геохимией мерзлоты"
	refereeSecret = "ТАЙНОЕ-ПИСЬМО-РЕКОМЕНДАТЕЛЯ"
)

func build(t *testing.T, w *world) built {
	t.Helper()
	b := built{me: w.Applicant("Мария Экспортова"), other: w.Applicant("Пётр Посторонний")}
	b.attachmentData = pdfData("MY-ATTACHMENT")
	b.attachment = w.Stored("../../etc/Статья: о катализе?.pdf", b.attachmentData)
	mine := w.apply(b.me, myCover, "ref-mine@example.ru", b.attachment)
	b.meApp = mine.ID
	other := w.apply(b.other, otherCover, "ref-other@example.ru", w.pdf("Чужая.pdf", "OTHER-ATTACHMENT"))
	b.otherID = other.ID
	if _, err := w.Prof.SetPrivacy(bg, b.me.User, "public", true); err != nil {
		t.Fatal(err)
	}
	// Решение организации, письмо рекомендателя (текст и файл) — соискатель увидит записку, но не письмо.
	exec(t, `UPDATE applications SET status = 'rejected', decision_note = 'Благодарим за интерес', decided_by = $2 WHERE id = $1`, b.meApp, w.tm.HR.ID)
	exec(t, `UPDATE reference_requests SET status = 'received', letter_text = $2, answered_at = now() WHERE application_id = $1`, b.meApp, refereeSecret)
	letter := w.Stored("letter.pdf", []byte("%PDF-1.7\n"+refereeSecret))
	exec(t, `INSERT INTO application_files (application_id, reference_id, kind, name, size, position, storage_key, sha256, scan_status, scanned_at, created_at)
		SELECT $1, r.id, 'reference_letter', 'letter.pdf', 20, 9, $2, $3, 'clean', now(), now() FROM reference_requests r WHERE r.application_id = $1`, b.meApp, letter.Key, letter.Sum[:])
	second := w.tm.Published(&w.tm.UnitB.ID)
	exec(t, `INSERT INTO favorites (user_id, vacancy_id, created_at) VALUES ($1, $2, now())`, b.me.ID, second.ID)
	exec(t, `INSERT INTO saved_searches (user_id, name, query, checked_at, next_run_at, created_at, updated_at) VALUES ($1, 'Катализ в Новосибирске', 'q=катализ', now(), now(), now(), now())`, b.me.ID)
	exec(t, `INSERT INTO deadline_reminders (user_id, vacancy_id, stage, deadline, sent_at) VALUES ($1, $2, 7, current_date, now())`, b.me.ID, second.ID)
	exec(t, `INSERT INTO notification_settings (user_id, email_new_vacancies, email_deadlines, updated_at) VALUES ($1, false, true, now())`, b.me.ID)
	exec(t, `INSERT INTO notifications (user_id, kind, title, body, created_at) VALUES ($1, 'test', 'Ваше уведомление', 'Тело уведомления', now()), ($2, 'test', 'Чужое уведомление', 'Тело чужого', now())`, b.me.ID, b.other.ID)
	exec(t, `INSERT INTO vacancy_offers (vacancy_id, user_id, invited_by, message, created_at, updated_at) VALUES ($1, $2, $3, 'Приходите к нам в лабораторию', now(), now())`, second.ID, b.me.ID, w.tm.Owner.ID)
	exec(t, `INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at) VALUES ($1, 'Ожидающее письмо мне', 'Ссылка для вас', now(), now()), ($2, 'Письмо постороннему', 'Чужая ссылка', now(), now())`, b.me.Email, b.other.Email)
	exec(t, `INSERT INTO known_devices (user_id, device_hash, first_seen_at, last_seen_at) VALUES ($1, 'devhash-secret', now(), now())`, b.me.ID)
	// Организации: человек сотрудник и ему пришло приглашение в другую.
	exec(t, `INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'hr', now())`, w.tm.OrgID, b.me.ID)
	exec(t, `INSERT INTO org_invitations (org_id, email, role, token_hash, invited_by, created_at, expires_at) VALUES ($1, $2, 'hr', $3, $4, now(), now() + interval '7 days')`, w.tm.OrgID, b.me.Email, []byte("invite-hash-secret-"+uuid.NewString()), w.tm.Owner.ID)
	// Журнал: вход человека с адресом и действие организации над ним.
	fresh(t, w.World, b.me.Email)
	exec(t, `INSERT INTO audit_log (at, org_id, actor_id, subject_id, action, target_kind, target_label, ip) VALUES (now(), $1, $2, $3, 'member.role_changed', 'member', 'Лаборатория А', '203.0.113.77')`, w.tm.OrgID, w.tm.Owner.ID, b.me.ID)
	exec(t, `INSERT INTO audit_log (at, org_id, actor_id, action, target_kind, target_label, ip) VALUES (now(), $1, $2, 'vacancy.published', 'vacancy', 'Чужая вакансия', '203.0.113.88')`, w.tm.OrgID, w.tm.Owner.ID)
	return b
}

type archive struct {
	entries map[string][]byte
	names   []string
	doc     map[string]json.RawMessage
}

func (a archive) section(t *testing.T, key string, into any) {
	t.Helper()
	raw, ok := a.doc[key]
	if !ok {
		t.Fatalf("section %q is missing from data.json (have %v)", key, slices.Sorted(keysOf(a.doc)))
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("section %q: %v\n%s", key, err, raw)
	}
}

func keysOf[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// all — весь текст архива, кроме файлов: для поиска того, чего там быть не должно.
func (a archive) all() string {
	return string(a.entries["data.json"]) + string(a.entries["README.txt"])
}

func open(t *testing.T, raw []byte) archive {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	a := archive{entries: map[string][]byte{}}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		a.entries[f.Name] = data
		a.names = append(a.names, f.Name)
	}
	if err := json.Unmarshal(a.entries["data.json"], &a.doc); err != nil {
		t.Fatalf("data.json: %v", err)
	}
	return a
}

func export(t *testing.T, svc *dataexport.Service, p auth.Principal) archive {
	t.Helper()
	arch, err := svc.Prepare(bg, p)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := svc.Write(bg, &buf, arch); err != nil {
		t.Fatal(err)
	}
	return open(t, buf.Bytes())
}

func (w *world) service() *dataexport.Service {
	return dataexport.NewService(testkit.Pool, w.Vault, dataexport.DefaultConfig("SciBox"))
}

func TestArchiveHoldsEverythingAboutThePersonAndNothingAboutOthers(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	a := export(t, w.service(), fresh(t, w.World, b.me.Email))

	// Состав: README, data.json и файлы только в files/.
	for _, name := range a.names {
		if name != "README.txt" && name != "data.json" && !strings.HasPrefix(name, "files/") {
			t.Errorf("unexpected entry %q", name)
		}
	}
	// Разделы data.json: все объявленные, в объявленном порядке, и ничего сверх.
	var keys []string
	dec := json.NewDecoder(bytes.NewReader(a.entries["data.json"]))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	if want := append([]string{"format", "exported_at"}, dataexport.Sections()...); !slices.Equal(keys, want) {
		t.Fatalf("sections = %v, want %v", keys, want)
	}

	var account struct {
		ID, Email, Name string
		PrivacyConsent  *time.Time `json:"privacy_consent_at"`
		PolicyVersion   string     `json:"privacy_policy_version"`
	}
	a.section(t, "account", &account)
	if account.ID != b.me.ID.String() || account.Email != b.me.Email || account.Name != "Мария Экспортова" || account.PrivacyConsent == nil || account.PolicyVersion == "" {
		t.Errorf("account = %+v", account)
	}
	var profile map[string]any
	a.section(t, "profile", &profile)
	if profile["headline"] != "Научный сотрудник, лаборатория катализа" || profile["visibility"] != "public" || profile["open_to_offers"] != true {
		t.Errorf("profile = %v", profile)
	}
	if _, has := profile["user_id"]; has {
		t.Error("the profile must not repeat the account number")
	}
	var specialties []map[string]string
	a.section(t, "profile_specialties", &specialties)
	if len(specialties) != 1 || specialties[0]["code"] != "1.4.4" || specialties[0]["name"] == "" {
		t.Errorf("specialties = %v", specialties)
	}

	var apps []struct {
		ID           string
		Vacancy      map[string]string
		Status       string
		CoverLetter  string         `json:"cover_letter"`
		ContactEmail string         `json:"contact_email"`
		Decision     string         `json:"decision_note"`
		Snapshot     map[string]any `json:"profile_snapshot"`
		Referees     []map[string]any
	}
	a.section(t, "applications", &apps)
	if len(apps) != 1 {
		t.Fatalf("applications = %d, want 1", len(apps))
	}
	got := apps[0]
	if got.ID != b.meApp.String() || got.CoverLetter != myCover || got.ContactEmail != b.me.Email || got.Status != "rejected" || got.Decision != "Благодарим за интерес" || got.Vacancy["title"] == "" || got.Vacancy["organization"] == "" {
		t.Errorf("application = %+v", got)
	}
	if got.Snapshot["headline"] != "Научный сотрудник, лаборатория катализа" {
		t.Errorf("the snapshot of the profile sent with the application is missing: %v", got.Snapshot)
	}
	if len(got.Referees) != 1 || got.Referees[0]["email"] != "ref-mine@example.ru" || got.Referees[0]["status"] != "received" {
		t.Errorf("referees = %v", got.Referees)
	}
	for _, r := range got.Referees {
		if _, leaks := r["letter_text"]; leaks {
			t.Error("the text of a reference letter must not be exported")
		}
	}

	var counts = map[string]int{}
	for _, key := range []string{"offers", "favorites", "saved_searches", "deadline_reminders", "notifications", "organizations", "organization_invitations", "waiting_mail", "known_browsers"} {
		var list []map[string]any
		a.section(t, key, &list)
		counts[key] = len(list)
	}
	// Точное число там, где данные заведены только тестом; вход по паролю сам добавляет письмо о новом устройстве,
	// уведомление и знакомый браузер, поэтому в этих разделах не меньше одной записи.
	for key, want := range map[string]int{"offers": 1, "favorites": 1, "saved_searches": 1, "deadline_reminders": 1, "organizations": 1, "organization_invitations": 1} {
		if counts[key] != want {
			t.Errorf("%s: %d records, want %d", key, counts[key], want)
		}
	}
	for _, key := range []string{"notifications", "waiting_mail", "known_browsers"} {
		if counts[key] < 1 {
			t.Errorf("%s: no records", key)
		}
	}
	if !strings.Contains(a.all(), "Ожидающее письмо мне") || !strings.Contains(a.all(), "Ваше уведомление") {
		t.Error("the person's own waiting mail and notification must be in the archive")
	}
	var settings map[string]any
	a.section(t, "notification_settings", &settings)
	if settings["email_new_vacancies"] != false || settings["email_deadlines"] != true {
		t.Errorf("settings = %v", settings)
	}
	var sessions []map[string]any
	a.section(t, "sessions", &sessions)
	if len(sessions) < 1 {
		t.Error("no sessions listed")
	}

	// Журнал: свои действия с адресом, чужие действия над собой без адреса и без имени сделавшего.
	var journal []struct {
		Action       string
		YouWere      string `json:"you_were"`
		Organization *string
		IP           *string `json:"ip"`
		TargetLabel  string  `json:"target_label"`
	}
	a.section(t, "journal", &journal)
	actions := map[string]string{}
	for _, e := range journal {
		actions[e.Action] += e.YouWere + ","
		if e.Action == "member.role_changed" && (e.IP != nil || e.YouWere != "subject" || e.Organization == nil) {
			t.Errorf("an action done to the person must show no address: %+v", e)
		}
		if e.Action == "account.login" && (e.IP == nil || e.YouWere != "actor") {
			t.Errorf("the person's own login must show the address: %+v", e)
		}
		if e.TargetLabel == "Чужая вакансия" {
			t.Errorf("an action of someone else on an unrelated object must not be exported: %+v", e)
		}
	}
	if !strings.Contains(actions["account.login"], "actor") || !strings.Contains(actions["member.role_changed"], "subject") || !strings.Contains(actions["account.data_exported"], "actor") {
		t.Errorf("journal actions = %v", actions)
	}

	// Файлы: резюме и приложение; письмо рекомендателя в архив не попадает; имя файла безопасное.
	var listed []dataexport.FileEntry
	a.section(t, "files", &listed)
	kinds := map[string]int{}
	for _, f := range listed {
		kinds[f.Kind]++
		data, ok := a.entries[f.Path]
		if !ok {
			t.Fatalf("file %s is listed but not in the archive", f.Path)
		}
		if strings.Contains(f.Path, "..") || strings.ContainsAny(strings.TrimPrefix(f.Path, "files/"+f.ApplicationID.String()+"/"), `\:*?"<>|`) {
			t.Errorf("unsafe path in the archive: %q", f.Path)
		}
		if f.Kind == "attachment" && !bytes.Equal(data, b.attachmentData) {
			t.Errorf("the attachment differs from the uploaded file")
		}
		if f.Kind == "cv" && !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("the CV is not a PDF")
		}
	}
	if kinds["cv"] != 1 || kinds["attachment"] != 1 || kinds["reference_letter"] != 0 {
		t.Errorf("files by kind = %v", kinds)
	}
	for name, data := range a.entries {
		if bytes.Contains(data, []byte(refereeSecret)) {
			t.Errorf("the reference letter reached the archive (%s)", name)
		}
	}

	// Чужого и секретного нет нигде.
	text := a.all()
	for _, leaked := range []string{
		b.other.Email, "Пётр Посторонний", otherCover, "ref-other@example.ru", "Тело чужого", "Чужая ссылка", "Чужая вакансия", "203.0.113.88",
		refereeSecret, "password_hash", "token_hash", "device_hash", "devhash-secret", "invite-hash-secret", "$argon2",
		w.tm.Owner.Name, w.tm.HR.Name, w.tm.Owner.Email, w.tm.HR.Email,
	} {
		if leaked != "" && strings.Contains(text, leaked) {
			t.Errorf("%q must not be in the archive", leaked)
		}
	}
	if !strings.Contains(string(a.entries["README.txt"]), "Рекомендательных писем о вас") {
		t.Error("README must explain what is not included")
	}
}

func TestArchiveOfAPersonWithNothingIsStillValid(t *testing.T) {
	w := newWorld(t)
	p := w.User("Пустой Человек")
	a := export(t, w.service(), fresh(t, w.World, p.Email))
	var profile json.RawMessage
	a.section(t, "profile", &profile)
	if string(profile) != "null" {
		t.Errorf("a person without a profile has profile = %s", profile)
	}
	for _, key := range []string{"applications", "offers", "favorites", "saved_searches", "notifications", "organizations", "files"} {
		var list []any
		a.section(t, key, &list)
		if list == nil || len(list) != 0 {
			t.Errorf("%s = %v, want an empty list", key, list)
		}
	}
	if len(a.names) != 2 {
		t.Errorf("entries = %v, want README and data.json", a.names)
	}
}

func TestExportNeedsAFreshPassword(t *testing.T) {
	w := newWorld(t)
	p := w.User("Свежий Пароль")
	stale, err := w.Auth.Authenticate(bg, p.Token)
	if err != nil || stale.Fresh {
		t.Fatalf("the session made by following a link must not be fresh: %+v %v", stale, err)
	}
	before := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'data_export' AND key = $1`, p.ID.String())
	if _, err := w.service().Prepare(bg, stale); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("err = %v, want ErrReauthRequired", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'data_export' AND key = $1`, p.ID.String()); n != before {
		t.Error("a refused request must not spend an attempt")
	}
	if rows := testaudit.Rows(t, testkit.Pool, `action = 'account.data_exported' AND actor_id = $1`, p.ID); len(rows) != 0 {
		t.Error("a refused request must leave no trace in the journal")
	}
}

func TestExportIsLimitedPerDay(t *testing.T) {
	w := newWorld(t)
	p := w.User("Частый Скачиватель")
	principal := fresh(t, w.World, p.Email)
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := &now
	svc := dataexport.NewServiceWithClock(testkit.Pool, w.Vault, dataexport.DefaultConfig("SciBox"), func() time.Time { return *clock })
	for i := range 3 {
		if _, err := svc.Prepare(bg, principal); err != nil {
			t.Fatalf("export %d: %v", i+1, err)
		}
		*clock = clock.Add(time.Hour)
	}
	_, err := svc.Prepare(bg, principal)
	rl, ok := errors.AsType[*auth.RateLimitedError](err)
	if !ok {
		t.Fatalf("the 4th export in a day: err = %v, want a rate limit", err)
	}
	// Ждать надо до тех пор, пока первая выгрузка не выйдет за окно: 24 часа минус три прошедших часа.
	if want := 21 * time.Hour; rl.RetryAfter < want-time.Minute || rl.RetryAfter > want+time.Minute {
		t.Errorf("RetryAfter = %v, want about %v", rl.RetryAfter, want)
	}
	if rows := testaudit.Rows(t, testkit.Pool, `action = 'account.data_exported' AND actor_id = $1`, p.ID); len(rows) != 3 {
		t.Errorf("the journal has %d exports, want exactly the 3 allowed", len(rows))
	}
	*clock = clock.Add(22 * time.Hour)
	if _, err := svc.Prepare(bg, principal); err != nil {
		t.Errorf("after the window the export works again: %v", err)
	}
	// Предел у каждого свой.
	other := w.User("Другой Скачиватель")
	if _, err := svc.Prepare(bg, fresh(t, w.World, other.Email)); err != nil {
		t.Errorf("the limit of one person must not touch another: %v", err)
	}
}

func TestEveryExportIsInThePersonalJournalAndWithoutAJournalThereIsNoArchive(t *testing.T) {
	w := newWorld(t)
	p := w.User("Журналируемый")
	principal := fresh(t, w.World, p.Email)
	if _, err := w.service().Prepare(bg, principal); err != nil {
		t.Fatal(err)
	}
	rows := testaudit.Rows(t, testkit.Pool, `action = 'account.data_exported' AND actor_id = $1`, p.ID)
	if len(rows) != 1 || rows[0].Org != nil || rows[0].IP != nil {
		// Адреса нет: в этом вызове нет запроса, из которого его взять (его кладёт роутер, HTTP-тест ниже).
		t.Fatalf("journal = %+v", rows)
	}

	testaudit.Break(t, testkit.Pool)
	before := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'data_export' AND key = $1`, p.ID.String())
	if arch, err := w.service().Prepare(bg, principal); err == nil || arch != nil {
		t.Fatalf("the journal is down, yet the archive was prepared: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM rate_events WHERE kind = 'data_export' AND key = $1`, p.ID.String()); n != before {
		t.Error("an attempt that did not reach the journal must not be counted")
	}
}

func TestExportOfAStrangersFileIsImpossible(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	var fileID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT f.id FROM application_files f WHERE f.application_id = $1 AND f.kind = 'cv'`, b.otherID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	q := dbgen.New(testkit.Pool)
	if _, err := q.ExportFileKey(bg, dbgen.ExportFileKeyParams{ID: fileID, UserID: b.me.ID}); err == nil {
		t.Error("the query returned a file of another person")
	}
	if key, err := q.ExportFileKey(bg, dbgen.ExportFileKeyParams{ID: fileID, UserID: b.other.ID}); err != nil || key == nil || !strings.HasPrefix(*key, files.KeyPrefix) {
		t.Errorf("the owner must get the key of the file: %v", err)
	}
	var letterID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM application_files WHERE application_id = $1 AND kind = 'reference_letter'`, b.meApp).Scan(&letterID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ExportFileKey(bg, dbgen.ExportFileKeyParams{ID: letterID, UserID: b.me.ID}); err == nil {
		t.Error("a reference letter must not be exportable even by the person it is about")
	}
}

func TestWriteSkipsAFileThatVanishedAndFailsOnABrokenWriter(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	svc := w.service()
	arch, err := svc.Prepare(bg, fresh(t, w.World, b.me.Email))
	if err != nil {
		t.Fatal(err)
	}
	exec(t, `DELETE FROM application_files WHERE id = $1`, arch.Files[0].ID)
	var buf bytes.Buffer
	if err := svc.Write(bg, &buf, arch); err != nil {
		t.Fatalf("a vanished file must be skipped: %v", err)
	}
	if a := open(t, buf.Bytes()); len(a.names) != len(arch.Files)+2-1 {
		t.Errorf("entries = %v", a.names)
	}
	// Сбой записи в любом месте архива (заголовок, текст, файл, оглавление) — ошибка, а не «успех».
	for limit := 0; limit < buf.Len()-1; limit += 53 {
		if err := svc.Write(bg, &limitedWriter{left: limit}, arch); err == nil {
			t.Errorf("a writer that fails after %d bytes must make Write fail", limit)
		}
	}
}

// Сбой в самом начале: README и data.json больше буфера zip (4 КБ) и несжимаемы, поэтому ошибка видна уже на них.
func TestWriteFailsEarlyOnABrokenWriter(t *testing.T) {
	w := newWorld(t)
	random := make([]byte, 100_000)
	_, _ = rand.Read(random) // несжимаемые байты: README и data.json не поместятся в буфер zip
	svc := w.service()
	for name, a := range map[string]*dataexport.Archive{
		"big readme":    {Readme: random, JSON: []byte("{}")},
		"big data.json": {Readme: []byte("r"), JSON: random},
	} {
		if err := svc.Write(bg, &limitedWriter{left: 10}, a); err == nil {
			t.Errorf("%s: a writer that fails must make Write fail", name)
		}
	}
}

type limitedWriter struct{ left int }

var errWriter = errors.New("the client went away")

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		n := l.left
		l.left = 0
		return n, errWriter
	}
	l.left -= len(p)
	return len(p), nil
}

func TestEmptyConfigMeansThreeExportsADayNotNoLimit(t *testing.T) {
	w := newWorld(t)
	p := w.User("Без Настроек")
	principal := fresh(t, w.World, p.Email)
	svc := dataexport.NewService(testkit.Pool, w.Vault, dataexport.Config{})
	for i := range 3 {
		if _, err := svc.Prepare(bg, principal); err != nil {
			t.Fatalf("export %d: %v", i+1, err)
		}
	}
	if _, err := svc.Prepare(bg, principal); !errors.As(err, new(*auth.RateLimitedError)) {
		t.Errorf("the 4th export with an empty config: %v", err)
	}
}

func TestPrepareAndWriteDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	principal := fresh(t, w.World, b.me.Email)
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		return func(db testkit.DB) error {
			svc := dataexport.NewService(db, w.Vault, dataexport.Config{ProductName: "SciBox", Export: dataexport.Limit{Max: 1000, Window: time.Hour}})
			arch, err := svc.Prepare(bg, principal)
			if err != nil {
				return err
			}
			return svc.Write(bg, io.Discard, arch)
		}
	})
}

func TestFileNamesAreCleanedAndUniquePerApplication(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	a := export(t, w.service(), fresh(t, w.World, b.me.Email))
	var listed []dataexport.FileEntry
	a.section(t, "files", &listed)
	seen := map[string]bool{}
	for _, f := range listed {
		if seen[f.Path] {
			t.Errorf("duplicate path %s", f.Path)
		}
		seen[f.Path] = true
		if !strings.HasPrefix(f.Path, fmt.Sprintf("files/%s/", b.meApp)) {
			t.Errorf("file of the application %s lies at %s", b.meApp, f.Path)
		}
	}
	if !slices.ContainsFunc(listed, func(f dataexport.FileEntry) bool {
		return strings.HasSuffix(f.Path, "-Статья о катализе.pdf")
	}) {
		t.Errorf("the dangerous file name was not cleaned to a readable one: %v", listed)
	}
	if got := (&dataexport.Archive{}).FileName(); !strings.HasPrefix(got, "scibox-data-") || !strings.HasSuffix(got, ".zip") {
		t.Errorf("FileName() = %q", got)
	}
}

// Файл, который ещё проверяется или отвергнут, в архив не попадает, но в data.json он перечислен; пропавший объект
// проверенного файла — ошибка, а не тихо неполный архив.
func TestArchiveSkipsUncheckedFilesAndFailsOnAMissingObject(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	svc := w.service()
	var pendingID, infectedID uuid.UUID
	rows, err := testkit.Pool.Query(bg, `SELECT id FROM application_files WHERE application_id = $1 AND kind = 'attachment' ORDER BY position`, b.meApp)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 1 {
		t.Fatalf("attachments: %d", len(ids))
	}
	pendingID = ids[0]
	// Ещё одно приложение, найденное заражённым.
	extra := w.Stored("second.pdf", pdfData("second"))
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO application_files (application_id, kind, name, size, position, storage_key, sha256, scan_status, scanned_at, created_at)
		VALUES ($1, 'attachment', 'second.pdf', $2, 5, $3, $4, 'clean', now(), now()) RETURNING id`, b.meApp, extra.Size, extra.Key, extra.Sum[:]).Scan(&infectedID); err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE application_files SET scan_status = 'pending', scanned_at = NULL WHERE id = $1`, pendingID)
	exec(t, `UPDATE application_files SET scan_status = 'infected', storage_key = NULL WHERE id = $1`, infectedID)

	a := export(t, svc, fresh(t, w.World, b.me.Email))
	var listed []struct {
		ID   uuid.UUID `json:"id"`
		Scan string    `json:"scan"`
		Path string    `json:"path"`
	}
	if err := json.Unmarshal(a.doc["files"], &listed); err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]string{}
	for _, f := range listed {
		seen[f.ID] = f.Scan
		if f.Scan != "clean" && f.Path != "" {
			t.Errorf("file %s (%s) has a path in the archive: %q", f.ID, f.Scan, f.Path)
		}
		if f.Scan != "clean" {
			if _, ok := a.entries[f.Path]; ok {
				t.Errorf("an unchecked file is in the archive")
			}
		}
	}
	if seen[pendingID] != "pending" || seen[infectedID] != "infected" {
		t.Errorf("states in data.json: pending=%q infected=%q", seen[pendingID], seen[infectedID])
	}
	for name := range a.entries {
		if strings.Contains(name, "second") {
			t.Errorf("the rejected file is in the archive: %s", name)
		}
	}

	// Проверенный файл без объекта: архив не собирается молча.
	for _, k := range w.Store.Keys() {
		_ = w.Store.Delete(bg, k)
	}
	arch, err := svc.Prepare(bg, fresh(t, w.World, b.me.Email))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Write(bg, io.Discard, arch); err == nil {
		t.Error("an archive with a missing object was written without an error")
	}
}
