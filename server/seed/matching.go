package seed

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/auth"
	"scibox/server/internal/matching"
	"scibox/server/internal/notifications"
	"scibox/server/internal/vacancies"
)

// Демо-данные среза 11: у нескольких учёных есть избранное (вакансии из их «подходящих»), у части из них срок подачи
// близко, и по два-три сохранённых поиска с разной частотой писем. Всё создаёт настоящий сервис, поэтому проходит те же
// проверки, что и в приложении.

type demoFavorites struct {
	who string
	// count — сколько первых «подходящих» вакансий добавить в избранное.
	count int
	// soon — через сколько дней срок подачи у первых вакансий избранного (0 — срок не меняем). Так календарь сроков и
	// напоминания выглядят живыми в любой день загрузки.
	soon []int
}

var demoFavoriteSets = []demoFavorites{
	{"lebedeva", 4, []int{3, 9}},
	{"korolev", 4, []int{1, 5}},
	{"guseva", 3, []int{6}},
	{"zhukova", 3, nil},
	{"morozov", 3, []int{12}},
	{"tarasov", 2, nil},
}

type demoSearch struct {
	who, name, query, frequency string
}

var demoSearches = []demoSearch{
	{"lebedeva", "Геология, Иркутская область", "field=1.6&region=38", matching.FreqDaily},
	{"lebedeva", "Геология с жильём", "field=1.6&housing=1", matching.FreqWeekly},
	{"korolev", "Биология: конкурсы", "field=1.5&competition=1", matching.FreqDaily},
	{"korolev", "Аспирантура и стажировки в биологии", "field=1.5&type=phd&type=internship", matching.FreqInstant},
	{"guseva", "Молекулярная биология", "q=молекулярная+биология", matching.FreqDaily},
	{"zhukova", "Математика: преподавание", "field=1.1&type=teaching", matching.FreqWeekly},
	{"morozov", "Химия: аспирантура", "field=1.3&type=phd", matching.FreqOff},
}

func seedMatching(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, now time.Time) (favorites, searches int, err error) {
	notes := notifications.NewService(pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	svc := matching.NewService(pool, vacancies.NewService(pool, vacancies.DefaultConfig()), notes, matching.DefaultConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, f := range demoFavoriteSets {
		user := auth.User{ID: ids[f.who]}
		var have int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM favorites WHERE user_id = $1`, user.ID).Scan(&have); err != nil {
			return 0, 0, fmt.Errorf("seed matching: %w", err)
		}
		if have > 0 {
			continue
		}
		list, err := svc.Matches(ctx, user, f.count, 0)
		if err != nil {
			return 0, 0, fmt.Errorf("seed matching: matches of %s: %w", f.who, err)
		}
		for i, item := range list.Items {
			if err := svc.AddFavorite(ctx, user, item.Vacancy.ID); err != nil {
				return 0, 0, fmt.Errorf("seed matching: favorite of %s: %w", f.who, err)
			}
			favorites++
			if i < len(f.soon) {
				// Срок подачи считается от дня загрузки, как у остальных демо-вакансий.
				deadline := now.AddDate(0, 0, f.soon[i])
				if _, err := pool.Exec(ctx, `UPDATE vacancies SET deadline = $2::date WHERE id = $1`, item.Vacancy.ID, deadline); err != nil {
					return 0, 0, fmt.Errorf("seed matching: deadline: %w", err)
				}
			}
		}
	}
	for _, s := range demoSearches {
		user := auth.User{ID: ids[s.who]}
		var have int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM saved_searches WHERE user_id = $1 AND name = $2`, user.ID, s.name).Scan(&have); err != nil {
			return 0, 0, fmt.Errorf("seed matching: %w", err)
		}
		if have > 0 {
			continue
		}
		if _, err := svc.CreateSearch(ctx, user, matching.SearchInput{Name: s.name, Query: s.query, Frequency: s.frequency}); err != nil {
			return 0, 0, fmt.Errorf("seed matching: search %q: %w", s.name, err)
		}
		searches++
	}
	return favorites, searches, nil
}
