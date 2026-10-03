package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/auth"
	"scibox/server/internal/notifications"
	"scibox/server/internal/offers"
)

// Демо-приглашения (срез 10): организации зовут учёных из каталога на свои открытые вакансии, часть учёных уже ответила.
// Приглашения отправляются настоящим сервисом приглашений, поэтому проходят те же проверки, что и в приложении.
// Письма, попавшие в очередь, помечаются отправленными: демо-данные не рассылают почту.

type demoOffer struct {
	inviter, orgSlug, scientist string
	message                     string
	// answer: "interested" / "declined" — учёный уже ответил, пусто — приглашение ждёт ответа.
	answer, note string
}

var demoOffers = []demoOffer{
	{"kuznetsova", "baykalskiy-institut-ekologii-i-klimata", "lebedeva", "Ваш опыт в геологии и геохимии близок нашему проекту по изучению донных отложений Байкала. Будем рады обсудить сотрудничество.", "", ""},
	{"kuznetsova", "baykalskiy-institut-ekologii-i-klimata", "guseva", "Мы ищем исследователя с опытом полевых работ и молекулярных методов. Посмотрели ваш профиль и хотим познакомиться.", "interested", "Спасибо за приглашение! Откликнусь на этой неделе."},
	{"kuznetsova", "baykalskiy-institut-ekologii-i-klimata", "korolev", "Приглашаем вас присоединиться к лаборатории. Условия и состав команды описаны на странице вакансии.", "declined", "Спасибо, сейчас я в проекте до конца года. Вернусь к вопросу весной."},
	{"sokolov", "moskovskiy-institut-prikladnoy-matematiki", "zhukova", "Ваши работы по функциональному анализу пересекаются с задачами нашего отдела. Приглашаем откликнуться.", "", ""},
	{"sokolov", "moskovskiy-institut-prikladnoy-matematiki", "belov", "Ищем математика с опытом численных методов. Ваш профиль подходит под описание вакансии.", "interested", ""},
	{"zaitseva", "dalnevostochnyy-institut-okeanologii", "vasilev", "Мы расширяем группу морской химии и приглашаем вас присоединиться.", "", ""},
}

func seedOffers(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, now time.Time) (int, error) {
	notes := notifications.NewService(pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	svc := offers.NewService(pool, notes, offers.Config{Invite: offers.Limit{Max: 1000, Window: 24 * time.Hour}})

	var lastOutbox int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM outbox`).Scan(&lastOutbox); err != nil {
		return 0, fmt.Errorf("seed offers: %w", err)
	}
	created := 0
	for _, o := range demoOffers {
		inviter, scientist := auth.User{ID: ids[o.inviter]}, auth.User{ID: ids[o.scientist]}
		var existing int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM vacancy_offers WHERE user_id = $1 AND invited_by = $2`, scientist.ID, inviter.ID).Scan(&existing); err != nil {
			return 0, fmt.Errorf("seed offers: %w", err)
		}
		if existing > 0 {
			continue
		}
		var profileID, vacancyID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT id FROM profiles WHERE user_id = $1`, scientist.ID).Scan(&profileID); err != nil {
			return 0, fmt.Errorf("seed offers: profile of %s: %w", o.scientist, err)
		}
		// Вакансия организации, подходящая учёному по специальности; без отклика и приглашения этого человека.
		if err := pool.QueryRow(ctx, `
			SELECT v.id FROM vacancy_view v
			WHERE v.org_slug = $1 AND v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= $2::date)
			  AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.vacancy_id = v.id AND a.user_id = $3)
			  AND NOT EXISTS (SELECT 1 FROM vacancy_offers f WHERE f.vacancy_id = v.id AND f.user_id = $3)
			ORDER BY EXISTS (SELECT 1 FROM vacancy_specialties s JOIN profile_specialties ps ON ps.specialty_code = s.specialty_code
			                 JOIN profiles p ON p.id = ps.profile_id WHERE s.vacancy_id = v.id AND p.user_id = $3) DESC,
			         EXISTS (SELECT 1 FROM vacancy_specialties s JOIN profile_specialties ps ON split_part(ps.specialty_code, '.', 1) || '.' || split_part(ps.specialty_code, '.', 2)
			                                                    = split_part(s.specialty_code, '.', 1) || '.' || split_part(s.specialty_code, '.', 2)
			                 JOIN profiles p ON p.id = ps.profile_id WHERE s.vacancy_id = v.id AND p.user_id = $3) DESC,
			         v.title, v.id
			LIMIT 1`, o.orgSlug, now, scientist.ID).Scan(&vacancyID); err != nil {
			return 0, fmt.Errorf("seed offers: vacancy of %s for %s: %w", o.orgSlug, o.scientist, err)
		}
		offer, err := svc.Invite(ctx, inviter, offers.InviteInput{VacancyID: vacancyID, ProfileID: profileID, Message: o.message})
		if err != nil {
			return 0, fmt.Errorf("seed offers: %s -> %s: %w", o.inviter, o.scientist, err)
		}
		created++
		if o.answer != "" {
			if err := svc.Answer(ctx, scientist, offer.ID, offers.AnswerInput{Action: o.answer, Note: o.note}); err != nil {
				return 0, fmt.Errorf("seed offers: answer of %s: %w", o.scientist, err)
			}
		}
	}
	// Демо-данные не рассылают почту: письма, поставленные в очередь этим запуском, считаются отправленными.
	if _, err := pool.Exec(ctx, `UPDATE outbox SET sent_at = $2 WHERE id > $1 AND sent_at IS NULL`, lastOutbox, now); err != nil {
		return 0, fmt.Errorf("seed offers: %w", err)
	}
	return created, nil
}
