package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/orgs"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var bgctx = context.Background()

// must отправляет запрос от имени who и требует успеха; возвращает разобранный ответ.
func (f *fx) must(who testkit.Person, method, route string, params map[string]string, body any) map[string]any {
	f.t.Helper()
	c := kase{route: method + " " + route}
	r := f.w.send(c, call{params: params, body: body}, &who)
	if r.Code < 200 || r.Code >= 300 {
		f.t.Fatalf("fixture %s %s: %s", method, route, r)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(r.Raw), &out)
	return out
}

func (f *fx) mustForm(who testkit.Person, route string, data any, files ...[2]string) map[string]any {
	f.t.Helper()
	c := kase{route: "POST " + route}
	r := f.w.send(c, call{form: &formBody{data: data, files: files}}, &who)
	if r.Code < 200 || r.Code >= 300 {
		f.t.Fatalf("fixture POST %s: %s", route, r)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(r.Raw), &out)
	return out
}

func id(f *fx, m map[string]any, path ...string) uuid.UUID {
	f.t.Helper()
	var cur any = m
	for _, p := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			f.t.Fatalf("no %q in %v", p, m)
		}
		cur = obj[p]
	}
	s, _ := cur.(string)
	v, err := uuid.Parse(s)
	if err != nil {
		f.t.Fatalf("%v is not an id: %v", path, cur)
	}
	return v
}

func (f *fx) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.w.pool.Exec(bgctx, sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) tm() *testkit.Team { return f.w.tm }

// vacancy создаёт вакансию подразделения A и переводит её в нужное состояние (заготовщиком).
func (f *fx) vacancy(status string) uuid.UUID {
	f.t.Helper()
	k, tm := f.w.k, f.w.tm
	d, err := k.Vac.Create(bgctx, f.w.fixer.User, tm.Slug, testkit.VacancyInput(&tm.UnitA.ID))
	if err != nil {
		f.t.Fatal(err)
	}
	steps := map[string][]string{
		vacancies.StatusDraft: nil, vacancies.StatusPublished: {vacancies.StatusPublished},
		vacancies.StatusClosed:   {vacancies.StatusPublished, vacancies.StatusClosed},
		vacancies.StatusArchived: {vacancies.StatusPublished, vacancies.StatusClosed, vacancies.StatusArchived},
	}
	for _, to := range steps[status] {
		if _, err := k.Vac.SetStatus(bgctx, f.w.fixer.User, d.ID, to); err != nil {
			f.t.Fatalf("%s: %v", to, err)
		}
	}
	return d.ID
}

func (f *fx) unit() orgs.Unit {
	f.t.Helper()
	u, err := f.w.k.Orgs.CreateUnit(bgctx, f.w.fixer.User, f.tm().Slug, orgs.UnitInput{Name: fmt.Sprintf("Отдел %d", testkit.Seq()), Kind: orgs.UnitDivision})
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

// member добавляет в организацию нового сотрудника с ролью.
func (f *fx) member(role string) testkit.Person {
	f.t.Helper()
	p := f.w.k.User("Сотрудник " + role)
	f.exec(`INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, $3, now())`, f.tm().OrgID, p.ID, role)
	return p
}

func (f *fx) orgInvitation(email string) uuid.UUID {
	f.t.Helper()
	inv, err := f.w.k.Orgs.Invite(bgctx, auth.Principal{User: f.w.fixer.User, Fresh: true}, f.tm().Slug, orgs.InviteInput{Email: email, Role: "hr"})
	if err != nil {
		f.t.Fatal(err)
	}
	return inv.ID
}

const coverLetter = "Здравствуйте! Мой опыт работы с катализаторами подходит для вашей лаборатории."

func pdfFile(name string) [2]string { return [2]string{name, "%PDF-1.7\n" + strings.Repeat("p", 300)} }

// application — отклик субъекта на новую вакансию подразделения A с одним приложенным файлом.
type application struct{ id, vacancy, cv, file uuid.UUID }

func (f *fx) application() application {
	f.t.Helper()
	subj := f.Subject()
	vac := f.vacancy(vacancies.StatusPublished)
	res := f.mustForm(subj, "/api/applications", map[string]any{
		"vacancy_id": vac, "contact_email": subj.Email, "cover_letter": coverLetter, "referees": []any{},
	}, pdfFile("diploma.pdf"))
	a := application{id: id(f, res, "application", "id"), vacancy: vac}
	rows, err := f.w.pool.Query(bgctx, `SELECT id, kind FROM application_files WHERE application_id = $1`, a.id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var fid uuid.UUID
		var kind string
		if err := rows.Scan(&fid, &kind); err != nil {
			f.t.Fatal(err)
		}
		if kind == "cv" {
			a.cv = fid
		} else {
			a.file = fid
		}
	}
	return a
}

func (f *fx) interview(app uuid.UUID) uuid.UUID {
	f.t.Helper()
	res := f.must(f.w.fixer, "POST", "/api/applications/{id}/invitations", map[string]string{"id": app.String()}, map[string]any{
		"kind": "interview", "message": "Расскажем о проекте", "place_kind": "online", "place": "https://meet.example.org/room-1",
		"starts_at": time.Now().Add(48 * time.Hour).In(time.FixedZone("MSK", 3*3600)).Format(time.RFC3339),
	})
	return id(f, res, "invitation", "id")
}

// proposal — собеседование, на которое соискатель предложил другое время.
func (f *fx) proposal(app uuid.UUID) uuid.UUID {
	f.t.Helper()
	inv := f.interview(app)
	f.must(f.Subject(), "POST", "/api/applications/{id}/invitations/{invId}/answer", map[string]string{"id": app.String(), "invId": inv.String()}, map[string]any{
		"action": "propose", "proposed_at": time.Now().Add(96 * time.Hour).In(time.FixedZone("MSK", 3*3600)).Format(time.RFC3339), "note": "Удобнее позже",
	})
	return inv
}

// reference — просьба соискателя к рекомендателю; возвращает её номер и одноразовую ссылку из письма.
func (f *fx) reference(app uuid.UUID) (uuid.UUID, string) {
	f.t.Helper()
	email := fmt.Sprintf("referee%d@example.ru", testkit.Seq())
	res := f.must(f.Subject(), "POST", "/api/applications/{id}/references", map[string]string{"id": app.String()},
		map[string]any{"name": "Профессор Иванов", "email": email, "relation": "научный руководитель"})
	ref := id(f, res, "reference", "id")
	var body string
	if err := f.w.pool.QueryRow(bgctx, `SELECT body FROM outbox WHERE to_email = $1 ORDER BY id DESC LIMIT 1`, email).Scan(&body); err != nil {
		f.t.Fatal(err)
	}
	_, rest, _ := strings.Cut(body, "token=")
	token, _, _ := strings.Cut(rest, "\n")
	return ref, strings.TrimSpace(token)
}

// scientist — профиль субъекта в нужном режиме приватности; возвращает номер профиля.
func (f *fx) scientist(visibility string) uuid.UUID {
	f.t.Helper()
	subj := f.Subject()
	if _, err := f.w.k.Prof.SetPrivacy(bgctx, subj.User, visibility, false); err != nil {
		f.t.Fatal(err)
	}
	own, err := f.w.k.Prof.Own(bgctx, subj.User)
	if err != nil {
		f.t.Fatal(err)
	}
	return own.Profile.ID
}

// offer — приглашение субъекта на новую вакансию подразделения A.
func (f *fx) offer() (offerID, vacancyID uuid.UUID) {
	f.t.Helper()
	profile := f.scientist("public")
	vacancyID = f.vacancy(vacancies.StatusPublished)
	res := f.must(f.w.fixer, "POST", "/api/offers", nil, map[string]any{"vacancy_id": vacancyID, "profile_id": profile, "message": "Приходите к нам"})
	return id(f, res, "offer", "id"), vacancyID
}

func (f *fx) notification() uuid.UUID {
	f.t.Helper()
	var n uuid.UUID
	err := f.w.pool.QueryRow(bgctx, `INSERT INTO notifications (user_id, kind, title, body, link, created_at) VALUES ($1, 'application_viewed', 'Отклик просмотрен', '', '', now()) RETURNING id`,
		f.Subject().ID).Scan(&n)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fx) savedSearch() uuid.UUID {
	f.t.Helper()
	res := f.must(f.Subject(), "POST", "/api/saved-searches", nil, map[string]any{"name": "Химия", "query": "q=химия", "frequency": "daily"})
	return id(f, res, "search", "id")
}

func (f *fx) profileItem() uuid.UUID {
	f.t.Helper()
	res := f.must(f.Subject(), "POST", "/api/profile/items", nil, map[string]any{"kind": "publication", "title": "Статья", "authors": "Орлова Е.", "pub_type": "article", "year": 2023})
	return id(f, res, "item", "id")
}

// session — ещё один сеанс субъекта (его номер).
func (f *fx) session() uuid.UUID {
	f.t.Helper()
	subj := f.Subject()
	_, s, err := f.w.k.Auth.Login(bgctx, subj.Email, testkit.Password, testkit.Meta())
	if err != nil {
		f.t.Fatal(err)
	}
	var sid uuid.UUID
	if err := f.w.pool.QueryRow(bgctx, `SELECT id FROM sessions WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, s.Token).Scan(&sid); err != nil {
		f.t.Fatal(err)
	}
	return sid
}

// vacancyBody — тело запроса на создание вакансии подразделения A (или всей организации, если unit пусто).
func (f *fx) vacancyBody(withOrg bool) map[string]any {
	b := map[string]any{
		"title": "Старший научный сотрудник", "position_code": "senior_researcher",
		"summary":     "Исследования активных центров катализаторов методами операндо-спектроскопии.",
		"description": "Работа в команде: эксперименты, анализ данных, публикации.", "career_level": 3,
		"work_format": "onsite", "region_code": "54", "city": "Новосибирск", "rate_percent": 100,
		"contract_type": "fixed", "contract_months": 36, "specialties": []string{"1.4.4"}, "deadline": testkit.Deadline(90),
		"unit_id": f.tm().UnitA.ID,
	}
	if withOrg {
		b["organization"] = f.tm().Slug
	}
	return b
}
