package erasure_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/orgs"
	"scibox/server/internal/references"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

var bg = testkit.BG

type world struct {
	*testkit.World
	apps *applications.Service
	tm   *testkit.Team
	vac  vacancies.Detail
}

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	notes := notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	refs := references.NewService(testkit.Pool, notes, k.Vault, references.DefaultConfig("SciBox", "http://localhost:5173"))
	w := &world{World: k, apps: applications.NewService(testkit.Pool, k.Prof, refs, notes, k.Vault, applications.Config{Apply: applications.Limit{Max: 1000, Window: time.Hour}})}
	w.tm = k.Team()
	w.vac = w.tm.Published(&w.tm.UnitA.ID)
	return w
}

func (w *world) pdf(name string) files.Stored { w.T.Helper(); return w.PDF(name) }

func (w *world) apply(who testkit.Person, vacancy uuid.UUID, ups ...files.Stored) applications.Detail {
	w.T.Helper()
	d, err := w.apps.Apply(bg, who.User, applications.Input{
		VacancyID: vacancy, ContactEmail: who.Email,
		CoverLetter: "Здравствуйте! Мой опыт работы с катализаторами подходит для вашей лаборатории.",
		Referees:    []references.RefereeInput{{Name: "Профессор Иванов", Email: fmt.Sprintf("ref%d@example.ru", testkit.Seq()), Relation: "руководитель"}},
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

// erase стирает человека так, как это делает auth: строка аккаунта под блокировкой, всё одной транзакцией.
func erase(t *testing.T, id uuid.UUID) error {
	t.Helper()
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	q := dbgen.New(tx)
	u, err := q.LockUserRow(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := erasure.Erase(bg, q, u, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(bg)
}

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

// Всё, что принадлежит человеку, стёрто; отклики обезличены; чужие данные целы (T-11, D-142, D-143).
func TestEraseRemovesPersonalData(t *testing.T) {
	w := newWorld(t)
	who := w.Applicant("Удаляемый Соискатель")
	other := w.Applicant("Другой Соискатель")
	if _, err := w.Prof.SetPrivacy(bg, who.User, "public"); err != nil {
		t.Fatal(err)
	}
	open := w.apply(who, w.vac.ID, w.pdf("Статья.pdf"))
	second := w.tm.Published(&w.tm.UnitA.ID)
	accepted := w.apply(who, second.ID)
	exec(t, `UPDATE applications SET status = 'accepted', decision_note = 'Ждём вас, Удаляемый Соискатель', decided_by = $2 WHERE id = $1`, accepted.ID, w.tm.HR.ID)
	others := w.apply(other, w.vac.ID)

	// Уведомления организации об откликах человека (в тексте его имя) и об отклике другого человека.
	for _, id := range []uuid.UUID{open.ID, others.ID} {
		exec(t, `INSERT INTO notifications (user_id, kind, title, link, created_at) VALUES ($1, 'test', 'Новый отклик', $2, now())`, w.tm.HR.ID, "/candidates/"+id.String())
	}
	exec(t, `INSERT INTO favorites (user_id, vacancy_id, created_at) VALUES ($1, $2, now())`, who.ID, second.ID)
	exec(t, `INSERT INTO saved_searches (user_id, name, query, checked_at, next_run_at, created_at, updated_at) VALUES ($1, 'Катализ', 'q=катализ', now(), now(), now(), now())`, who.ID)
	exec(t, `INSERT INTO deadline_reminders (user_id, vacancy_id, stage, deadline, sent_at) VALUES ($1, $2, 7, current_date, now())`, who.ID, second.ID)
	exec(t, `INSERT INTO notification_settings (user_id, email_new_vacancies, email_deadlines, updated_at) VALUES ($1, false, true, now())`, who.ID)
	exec(t, `INSERT INTO notifications (user_id, kind, title, created_at) VALUES ($1, 'test', 'Ваше', now())`, who.ID)
	third := w.tm.Published(&w.tm.UnitB.ID)
	exec(t, `INSERT INTO vacancy_offers (vacancy_id, user_id, invited_by, message, created_at, updated_at) VALUES ($1, $2, $3, 'Приходите', now(), now())`, third.ID, who.ID, w.tm.Owner.ID)
	exec(t, `INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at) VALUES ($1, 'Вам письмо', 'Текст', now(), now()), ($2, 'Другому', 'Текст', now(), now())`, who.Email, other.Email)
	exec(t, `INSERT INTO rate_events (kind, key, at) VALUES ('login_email', $1, now()), ('password_user', $2, now()), ('login_email', $3, now())`, who.Email, who.ID.String(), other.Email)
	// Ему отправлено приглашение в организацию, и он уже руководитель подразделения в ней.
	if _, err := w.Orgs.Invite(bg, auth.Principal{User: w.tm.Owner.User, Fresh: true}, w.tm.Slug, orgs.InviteInput{Email: who.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: w.tm.OrgID, UserID: who.ID, Role: string(access.RoleUnitHead), JoinedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE units SET head_user_id = $1 WHERE id = $2`, who.ID, w.tm.UnitB.ID)
	oldEmail := who.Email
	before := othersSnapshot(t, w, other, others.ID)
	before.staff-- // сам удаляемый был сотрудником
	if before.application != 1 || before.files == 0 || before.notices < 2 || before.outbox == 0 || before.rateEvents != 1 || before.profile != 1 {
		t.Fatalf("the check of someone else's data would prove nothing: %+v", before)
	}

	myKeys := keysOf(t, who.ID)
	otherKeys := keysOf(t, other.ID)
	if len(myKeys) < 2 || len(otherKeys) == 0 {
		t.Fatalf("the check of the file objects would prove nothing: %d and %d keys", len(myKeys), len(otherKeys))
	}

	if err := erase(t, who.ID); err != nil {
		t.Fatal(err)
	}

	// Объекты файлов поставлены в очередь на удаление из хранилища, чужие нет. Сами объекты ещё лежат: их удалит
	// фоновая задача (filework), чтобы недоступное хранилище не откатывало стирание.
	for _, k := range myKeys {
		if n := testkit.Count(t, `SELECT count(*) FROM blob_deletions WHERE storage_key = $1`, k); n != 1 {
			t.Errorf("object %s is not queued for deletion (%d)", k, n)
		}
		if !w.Store.Has(k) {
			t.Errorf("object %s was deleted inside the database transaction", k)
		}
	}
	for _, k := range otherKeys {
		if n := testkit.Count(t, `SELECT count(*) FROM blob_deletions WHERE storage_key = $1`, k); n != 0 {
			t.Errorf("another person's object %s is queued for deletion", k)
		}
	}

	// Строка аккаунта без почты, имени и пароля.
	var email, name, hash string
	var deletedAt *time.Time
	if err := testkit.Pool.QueryRow(bg, `SELECT email, display_name, password_hash, deleted_at FROM users WHERE id = $1`, who.ID).Scan(&email, &name, &hash, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if email != "deleted+"+who.ID.String()+"@deleted" || name != "Аккаунт удалён" || hash != "!" || deletedAt == nil {
		t.Fatalf("tombstone: %q %q %q %v", email, name, hash, deletedAt)
	}
	if _, msg := auth.NormalizeEmail(email); msg == "" {
		t.Fatal("the tombstone address must be impossible to type into any form")
	}

	gone := map[string]string{
		"profile":              `SELECT count(*) FROM profiles WHERE user_id = $1`,
		"sessions":             `SELECT count(*) FROM sessions WHERE user_id = $1`,
		"auth tokens":          `SELECT count(*) FROM auth_tokens WHERE user_id = $1`,
		"known devices":        `SELECT count(*) FROM known_devices WHERE user_id = $1`,
		"favorites":            `SELECT count(*) FROM favorites WHERE user_id = $1`,
		"saved searches":       `SELECT count(*) FROM saved_searches WHERE user_id = $1`,
		"reminders":            `SELECT count(*) FROM deadline_reminders WHERE user_id = $1`,
		"settings":             `SELECT count(*) FROM notification_settings WHERE user_id = $1`,
		"own notifications":    `SELECT count(*) FROM notifications WHERE user_id = $1`,
		"offers":               `SELECT count(*) FROM vacancy_offers WHERE user_id = $1`,
		"memberships":          `SELECT count(*) FROM org_members WHERE user_id = $1`,
		"unit heads":           `SELECT count(*) FROM units WHERE head_user_id = $1`,
		"files":                `SELECT count(*) FROM application_files f JOIN applications a ON a.id = f.application_id WHERE a.user_id = $1`,
		"referees":             `SELECT count(*) FROM reference_requests r JOIN applications a ON a.id = r.application_id WHERE a.user_id = $1`,
		"candidate notices":    `SELECT count(*) FROM notifications n JOIN applications a ON n.link = '/candidates/' || a.id::text WHERE a.user_id = $1`,
		"not anonymized":       `SELECT count(*) FROM applications WHERE user_id = $1 AND (anonymized_at IS NULL OR cover_letter <> '' OR contact_email <> '' OR profile <> '{}'::jsonb OR decision_note <> '')`,
		"rate events (by id)":  `SELECT count(*) FROM rate_events WHERE key = $1::text`,
		"undecided still open": `SELECT count(*) FROM applications WHERE user_id = $1 AND status IN ('sent', 'viewed', 'invited')`,
	}
	for what, q := range gone {
		if n := testkit.Count(t, q, who.ID); n != 0 {
			t.Errorf("%s: %d rows left", what, n)
		}
	}
	for what, q := range map[string]string{
		"outbox":            `SELECT count(*) FROM outbox WHERE to_email = $1`,
		"org invitations":   `SELECT count(*) FROM org_invitations WHERE email = $1`,
		"rate events (key)": `SELECT count(*) FROM rate_events WHERE key = $1`,
	} {
		if n := testkit.Count(t, q, oldEmail); n != 0 {
			t.Errorf("%s: %d rows left", what, n)
		}
	}

	// Отклики остались: открытый стал отозванным, принятый остался принятым.
	statuses := map[uuid.UUID]string{}
	rows, err := testkit.Pool.Query(bg, `SELECT id, status FROM applications WHERE user_id = $1`, who.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id uuid.UUID
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatal(err)
		}
		statuses[id] = st
	}
	rows.Close()
	if statuses[open.ID] != "withdrawn" || statuses[accepted.ID] != "accepted" {
		t.Fatalf("statuses after erasure: %v", statuses)
	}

	// Чужое цело: отклик другого человека с файлами, уведомления о нём, письма и счётчики другого, сотрудники.
	if after := othersSnapshot(t, w, other, others.ID); after != before {
		t.Errorf("someone else's data changed: before %+v, after %+v", before, after)
	}

	// Карточка обезличенного отклика у организации: имени, письма и файлов нет, есть пометка.
	d, err := w.apps.Get(bg, w.tm.HR.User, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Anonymized || d.ApplicantName != "Аккаунт удалён" || d.CoverLetter != "" || d.ContactEmail != "" || d.CV != nil || len(d.Files) != 0 || d.Profile.Headline != "" {
		t.Fatalf("anonymized card: %+v", d.Base)
	}
}

type snapshot struct{ application, notices, outbox, rateEvents, profile, files, staff int }

func othersSnapshot(t *testing.T, w *world, other testkit.Person, app uuid.UUID) snapshot {
	t.Helper()
	return snapshot{
		application: testkit.Count(t, `SELECT count(*) FROM applications WHERE id = $1 AND anonymized_at IS NULL AND cover_letter <> ''`, app),
		notices:     testkit.Count(t, `SELECT count(*) FROM notifications WHERE link = $1`, "/candidates/"+app.String()),
		outbox:      testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1`, other.Email),
		rateEvents:  testkit.Count(t, `SELECT count(*) FROM rate_events WHERE key = $1`, other.Email),
		profile:     testkit.Count(t, `SELECT count(*) FROM profiles WHERE user_id = $1`, other.ID),
		files:       testkit.Count(t, `SELECT count(*) FROM application_files WHERE application_id = $1`, app),
		staff:       testkit.Count(t, `SELECT count(*) FROM org_members WHERE org_id = $1`, w.tm.OrgID),
	}
}

// Любая таблица со ссылкой на аккаунт: после стирания ссылаются только обезличенные отклики и записи «кто сделал».
// Новая таблица с user_id без строки в erasure провалит этот тест.
func TestEveryReferenceToTheAccountIsHandled(t *testing.T) {
	w := newWorld(t)
	who := w.Applicant("Проверка ссылок")
	w.apply(who, w.vac.ID)
	if err := erase(t, who.ID); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"applications.user_id":               true, // обезличенный отклик
		"applications.decided_by":            true, // кто принял решение
		"application_invitations.created_by": true,
		"organizations.created_by":           true,
		"vacancies.created_by":               true,
		"org_invitations.invited_by":         true,
		"vacancy_offers.invited_by":          true,
		"audit_log.actor_id":                 true, // история организации; адрес из записей стирается (TestErasureKeepsTheJournalButDropsAddresses)
		"audit_log.subject_id":               true,
	}
	rows, err := testkit.Pool.Query(bg, `
		SELECT kcu.table_name::text, kcu.column_name::text
		FROM information_schema.referential_constraints rc
		JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = rc.constraint_name AND kcu.constraint_schema = rc.constraint_schema
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = rc.unique_constraint_name AND ccu.constraint_schema = rc.unique_constraint_schema
		WHERE ccu.table_name = 'users' AND rc.constraint_schema = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, table+"."+column)
	}
	rows.Close()
	if len(refs) < 15 {
		t.Fatalf("found only %d references to users: %v", len(refs), refs)
	}
	for _, ref := range refs {
		table, column, _ := strings.Cut(ref, ".")
		n := testkit.Count(t, fmt.Sprintf(`SELECT count(*) FROM %q WHERE %q = $1`, table, column), who.ID)
		if n > 0 && !allowed[ref] {
			t.Errorf("%s still points to the erased account (%d rows): add it to internal/erasure", ref, n)
		}
	}
	for ref := range allowed {
		if !slices.Contains(refs, ref) {
			t.Errorf("allowed reference %s is not a foreign key any more: update the test", ref)
		}
	}
}

// Единственный владелец, а сотрудники есть: ничего не меняется (D-142).
func TestEraseBlockedWhileStaffRemain(t *testing.T) {
	w := newWorld(t)
	err := erase(t, w.tm.Owner.ID)
	blocked, ok := errors.AsType[*erasure.BlockedError](err)
	if !ok || len(blocked.Orgs) != 1 || blocked.Orgs[0].Slug != w.tm.Slug || !strings.Contains(blocked.Error(), w.tm.Slug) {
		t.Fatalf("Erase = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM users WHERE id = $1 AND deleted_at IS NULL AND email = $2`, w.tm.Owner.ID, w.tm.Owner.Email); n != 1 {
		t.Fatal("a blocked erasure must change nothing")
	}
}

// Единственный сотрудник: организация остаётся без владельца, опубликованные вакансии закрываются.
func TestEraseOrphansALoneOrganization(t *testing.T) {
	w := newWorld(t)
	lone := w.User("Одиночка")
	org, err := w.Orgs.CreateOrganization(bg, lone.User, orgs.OrgInput{Name: fmt.Sprintf("Лаборатория одного %d", testkit.Seq()), Kind: orgs.KindInstitute, City: "Томск"})
	if err != nil {
		t.Fatal(err)
	}
	in := testkit.VacancyInput(nil)
	v, err := w.Vac.Create(bg, lone.User, org.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Vac.SetStatus(bg, lone.User, v.ID, vacancies.StatusPublished); err != nil {
		t.Fatal(err)
	}
	draft, err := w.Vac.Create(bg, lone.User, org.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := erasure.Check(bg, dbgen.New(testkit.Pool), lone.ID)
	if err != nil || plan.Blocked() || len(plan.Orphaned) != 1 || plan.Orphaned[0].Slug != org.Slug {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if err := erase(t, lone.ID); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM vacancies WHERE id = $1 AND status = 'closed' AND closed_at IS NOT NULL`, v.ID); n != 1 {
		t.Fatal("the published vacancy must be closed")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM vacancies WHERE id = $1 AND status = 'draft'`, draft.ID); n != 1 {
		t.Fatal("a draft stays a draft")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM organizations WHERE id = $1`, org.ID); n != 1 {
		t.Fatal("the organization page stays")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM vacancies WHERE id = $1 AND status = 'published'`, w.vac.ID); n != 1 {
		t.Fatal("other organizations' vacancies must not change")
	}
}

// Совладелец не мешает: в плане нет ни мешающих, ни осиротевших организаций.
func TestCheckWithCoOwner(t *testing.T) {
	w := newWorld(t)
	exec(t, `UPDATE org_members SET role = 'owner' WHERE org_id = $1 AND user_id = $2`, w.tm.OrgID, w.tm.HR.ID)
	plan, err := erasure.Check(bg, dbgen.New(testkit.Pool), w.tm.Owner.ID)
	if err != nil || plan.Blocked() || len(plan.Orphaned) != 0 || len(plan.Blocking) != 0 {
		t.Fatalf("plan with a co-owner: %+v %v", plan, err)
	}
	// Сотрудник без роли владельца тоже ничего не держит.
	plan, err = erasure.Check(bg, dbgen.New(testkit.Pool), w.tm.HeadA.ID)
	if err != nil || plan.Blocked() || len(plan.Orphaned) != 0 {
		t.Fatalf("plan of a unit head: %+v %v", plan, err)
	}
}

// Сбой базы на любом шаге доходит до вызывающего (I-09): иначе часть данных осталась бы, а аккаунт считался стёртым.
func TestEraseDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	w := newWorld(t)
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		lone := w.User("Сбой")
		if _, err := w.Orgs.CreateOrganization(bg, lone.User, orgs.OrgInput{Name: fmt.Sprintf("Сбойная %d", testkit.Seq()), Kind: orgs.KindInstitute, City: "Омск"}); err != nil {
			t.Fatal(err)
		}
		w.FillProfile(lone)
		w.apply(lone, w.vac.ID)
		u, err := dbgen.New(testkit.Pool).GetUserByID(bg, lone.ID)
		if err != nil {
			t.Fatal(err)
		}
		return func(db testkit.DB) error { return erasure.Erase(bg, dbgen.New(db), u, time.Now().UTC()) }
	})
}

// keysOf — ключи объектов файлов откликов человека.
func keysOf(t *testing.T, user uuid.UUID) []string {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT f.storage_key FROM application_files f JOIN applications a ON a.id = f.application_id WHERE a.user_id = $1 AND f.storage_key IS NOT NULL ORDER BY f.storage_key`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	return keys
}
