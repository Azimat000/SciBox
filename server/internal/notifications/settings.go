package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// Settings — какие письма человек хочет получать. Уведомления на сайте от этого не зависят.
type Settings struct {
	// EmailNewVacancies — письма о новых вакансиях по сохранённым поискам.
	EmailNewVacancies bool `json:"email_new_vacancies"`
	// EmailDeadlines — напоминания о сроках подачи вакансий из избранного.
	EmailDeadlines bool `json:"email_deadlines"`
}

// Settings отдаёт настройки писем человека; пока он их не менял, включено всё.
func (s *Service) Settings(ctx context.Context, user auth.User) (Settings, error) {
	row, err := s.q.GetNotificationSettings(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{EmailNewVacancies: true, EmailDeadlines: true}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("notifications: load settings: %w", err)
	}
	return Settings{EmailNewVacancies: row.EmailNewVacancies, EmailDeadlines: row.EmailDeadlines}, nil
}

// SaveSettings запоминает настройки целиком.
func (s *Service) SaveSettings(ctx context.Context, user auth.User, in Settings) (Settings, error) {
	err := s.q.UpsertNotificationSettings(ctx, dbgen.UpsertNotificationSettingsParams{
		UserID: user.ID, EmailNewVacancies: in.EmailNewVacancies, EmailDeadlines: in.EmailDeadlines, Now: s.now(),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("notifications: save settings: %w", err)
	}
	return in, nil
}
