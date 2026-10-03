package seed

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/cv"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/references"
)

// Демо-отклики (срез 8): учёные из seedScientists откликаются на вакансии своей области, у части откликов есть
// статусы, приложенные файлы и рекомендатели (письмо получено, ждём ответа, отказался). Отклики отправляются через
// настоящий сервис откликов, поэтому проходят те же проверки, что и в приложении. Статусы «просмотрен», «приглашение»,
// «отказ», «принят» ставятся напрямую в базе: до среза 9 организация сама менять их не может.
// Письма, которые при этом попали в очередь, помечаются отправленными: демо-данные не рассылают почту.

type demoApplicant struct {
	key      string
	statuses []string // по одному отклику на статус
}

var demoApplicants = []demoApplicant{
	{"korolev", []string{applications.StatusSent, applications.StatusViewed, applications.StatusInvited, applications.StatusRejected, applications.StatusAccepted}},
	{"lebedeva", []string{applications.StatusSent, applications.StatusWithdrawn}},
	{"morozov", []string{applications.StatusSent, applications.StatusViewed}},
	{"zhukova", []string{applications.StatusInvited}},
	{"shiryaev", []string{applications.StatusSent}},
	{"guseva", []string{applications.StatusViewed, applications.StatusRejected}},
	{"tarasov", []string{applications.StatusSent}},
	{"andreeva", []string{applications.StatusSent}},
}

type demoReferee struct {
	name, email, relation string
	// answer: "text" — письмо текстом, "pdf" — PDF и текст, "decline" — отказался, "" — ещё не ответил.
	answer string
}

// demoReferees — рекомендатели для отдельных откликов: ключ «человек#номер отклика».
var demoReferees = map[string][]demoReferee{
	"korolev#0": {
		{"Андрей Козлов", "a.kozlov@kfu.example.ru", "научный руководитель", "pdf"},
		{"Лариса Миронова", "l.mironova@bio.example.ru", "коллега по лаборатории", ""},
	},
	"korolev#1": {{"Павел Ермаков", "p.ermakov@genome.example.ru", "соавтор", "decline"}},
	"guseva#0":  {{"Татьяна Орехова", "t.orehova@hse.example.ru", "научный руководитель", "text"}},
	"tarasov#0": {{"Борис Лапин", "b.lapin@mephi.example.ru", "заведующий кафедрой", "text"}},
}

var demoLetters = map[string]string{
	"text": "Работаю с кандидатом несколько лет и могу сказать, что это исследователь с редким сочетанием аккуратности и инициативы. Рекомендую без оговорок.",
	"pdf":  "Письмо во вложении. Коротко: самостоятельный, надёжный, быстро берёт на себя ответственность за целое направление.",
}

// demoPDF рисует небольшой PDF с названием и абзацем текста: приложение к отклику.
func demoPDF(title, text string) []byte {
	b, err := cv.Render(cv.Document{Title: title, Sections: []cv.Section{{Heading: "Содержание", Paragraph: text}}, Footer: "Демонстрационный файл", Created: time.Now()})
	if err != nil {
		panic(fmt.Sprintf("seed: demo pdf: %v", err)) // шрифты встроены в программу: сбой здесь значит сломанную сборку
	}
	return b
}

// lowerFirst: «Научный сотрудник» → «научный сотрудник» (по знакам, а не по байтам).
func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToLower(string(r)) + s[n:]
}

// groupPrefix: «1.5.8» → «1.5.%» (вся группа специальностей).
func groupPrefix(code string) string {
	parts := strings.Split(code, ".")
	if len(parts) < 2 {
		return code + ".%"
	}
	return parts[0] + "." + parts[1] + ".%"
}

func seedApplications(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, now time.Time) (int, error) {
	notes := notifications.NewService(pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	refSvc := references.NewService(pool, notes, references.DefaultConfig("SciBox", "http://localhost:5173"))
	profSvc := newProfiles(pool)
	appSvc := applications.NewService(pool, profSvc, refSvc, notes, applications.Config{Apply: applications.Limit{Max: 1000, Window: 24 * time.Hour}})

	var lastOutbox int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM outbox`).Scan(&lastOutbox); err != nil {
		return 0, fmt.Errorf("seed applications: %w", err)
	}
	created := 0
	for _, a := range demoApplicants {
		user := auth.User{ID: ids[a.key]}
		var existing int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM applications WHERE user_id = $1`, user.ID).Scan(&existing); err != nil {
			return 0, fmt.Errorf("seed applications: %w", err)
		}
		if existing > 0 {
			continue
		}
		var (
			name, email, headline string
			spec                  string
		)
		if err := pool.QueryRow(ctx, `SELECT u.display_name, u.email, p.headline, COALESCE((SELECT min(specialty_code) FROM profile_specialties WHERE profile_id = p.id), '')
			FROM users u JOIN profiles p ON p.user_id = u.id WHERE u.id = $1`, user.ID).Scan(&name, &email, &headline, &spec); err != nil {
			return 0, fmt.Errorf("seed applications: profile of %s: %w", a.key, err)
		}
		user.Name, user.Email = name, email
		vacancies, err := pickVacancies(ctx, pool, groupPrefix(spec), len(a.statuses), now)
		if err != nil {
			return 0, err
		}
		for i, status := range a.statuses {
			if i >= len(vacancies) {
				break
			}
			v := vacancies[i]
			in := applications.Input{
				VacancyID: v.id, ContactEmail: email,
				CoverLetter: fmt.Sprintf("Здравствуйте! Меня заинтересовала вакансия «%s». Мой профиль: %s. Буду признателен за возможность обсудить задачи подробнее и рассказать о своём опыте.", v.title, lowerFirst(headline)),
			}
			var refs []demoReferee
			for _, r := range demoReferees[fmt.Sprintf("%s#%d", a.key, i)] {
				refs = append(refs, r)
				in.Referees = append(in.Referees, references.RefereeInput{Name: r.name, Email: r.email, Relation: r.relation})
			}
			var ups []files.Upload
			if i == 0 {
				ups = append(ups, files.Upload{Name: "Список публикаций.pdf", Data: demoPDF("Список публикаций", "Избранные работы за последние пять лет; полный список в профиле.")})
			}
			d, err := appSvc.Apply(ctx, user, in, ups)
			if err != nil {
				return 0, fmt.Errorf("seed applications: %s -> %q: %w", a.key, v.title, err)
			}
			created++
			if err := answerReferees(ctx, pool, refSvc, d.ID, refs); err != nil {
				return 0, err
			}
			if status != applications.StatusSent {
				if _, err := pool.Exec(ctx, `UPDATE applications SET status = $2, status_changed_at = created_at + interval '2 days', updated_at = created_at + interval '2 days' WHERE id = $1`, d.ID, status); err != nil {
					return 0, fmt.Errorf("seed applications: set status: %w", err)
				}
			}
		}
	}
	// Демо-данные не рассылают почту: письма, поставленные в очередь этим запуском, считаются отправленными.
	if _, err := pool.Exec(ctx, `UPDATE outbox SET sent_at = $2 WHERE id > $1 AND sent_at IS NULL`, lastOutbox, now); err != nil {
		return 0, fmt.Errorf("seed applications: %w", err)
	}
	return created, nil
}

type pickedVacancy struct {
	id    uuid.UUID
	title string
}

// pickVacancies берёт открытые вакансии группы специальностей: в стабильном порядке, чтобы повторная загрузка давала то же.
func pickVacancies(ctx context.Context, pool *pgxpool.Pool, prefix string, n int, now time.Time) ([]pickedVacancy, error) {
	rows, err := pool.Query(ctx, `
		SELECT v.id, v.title FROM vacancy_view v
		WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= $1::date)
		  AND EXISTS (SELECT 1 FROM vacancy_specialties s WHERE s.vacancy_id = v.id AND s.specialty_code LIKE $2)
		ORDER BY v.org_name, v.title, v.id
		LIMIT $3 OFFSET 2`, now, prefix, n)
	if err != nil {
		return nil, fmt.Errorf("seed applications: pick vacancies: %w", err)
	}
	defer rows.Close()
	var out []pickedVacancy
	for rows.Next() {
		var v pickedVacancy
		if err := rows.Scan(&v.id, &v.title); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// answerReferees отвечает за рекомендателей так, как задано в демо-данных. Ссылку берём из письма в очереди.
func answerReferees(ctx context.Context, pool *pgxpool.Pool, refSvc *references.Service, appID uuid.UUID, refs []demoReferee) error {
	for _, r := range refs {
		if r.answer == "" {
			continue
		}
		var body string
		if err := pool.QueryRow(ctx, `SELECT body FROM outbox WHERE to_email = $1 ORDER BY id DESC LIMIT 1`, r.email).Scan(&body); err != nil {
			return fmt.Errorf("seed applications: mail to %s: %w", r.email, err)
		}
		i := strings.Index(body, "/recommend?token=")
		token := strings.Fields(body[i+len("/recommend?token="):])[0]
		var err error
		switch r.answer {
		case "decline":
			err = refSvc.Decline(ctx, token)
		case "pdf":
			err = refSvc.Submit(ctx, token, references.LetterInput{Text: demoLetters["pdf"]}, &files.Upload{Name: "Рекомендация.pdf", Data: demoPDF("Рекомендательное письмо", demoLetters["text"])})
		default:
			err = refSvc.Submit(ctx, token, references.LetterInput{Text: demoLetters[r.answer]}, nil)
		}
		if err != nil {
			return fmt.Errorf("seed applications: referee %s of %s: %w", r.email, appID, err)
		}
	}
	return nil
}
