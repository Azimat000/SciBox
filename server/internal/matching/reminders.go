package matching

import (
	"context"
	"errors"
	"fmt"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
)

// SendReminders делает один проход напоминаний о сроках (D-105) и возвращает, сколько отправлено. Напоминание уходит не
// раньше девяти утра по Москве, один раз на человека, вакансию, ступень (за неделю или за сутки) и срок; тем, кто уже
// откликнулся, и тем, кто добавил вакансию в избранное только что, не уходит (см. ListReminderCandidates).
func (s *Service) SendReminders(ctx context.Context) (int, error) {
	now := s.now()
	if !reminderHourOK(now) {
		return 0, nil
	}
	today := moscowToday(now)
	sent := 0
	var errs []error
	for pass := 0; pass < maxPasses; pass++ {
		rows, err := s.q.ListReminderCandidates(ctx, dbgen.ListReminderCandidatesParams{
			Today: today, Horizon: today.AddDate(0, 0, reminderHorizonDays), Batch: int32(s.cfg.Batch),
		})
		if err != nil {
			return sent, errors.Join(append(errs, fmt.Errorf("matching: list reminders: %w", err))...)
		}
		failed := 0
		for _, r := range rows {
			err := s.inTx(ctx, func(q *dbgen.Queries) error {
				n, err := q.InsertReminder(ctx, dbgen.InsertReminderParams{UserID: r.UserID, VacancyID: r.VacancyID, Stage: r.Stage, Deadline: r.Deadline, Now: now})
				if err != nil {
					return fmt.Errorf("matching: record reminder: %w", err)
				}
				if n == 0 {
					return nil // другой проход успел раньше
				}
				title, body := reminderNotice(r.Title, r.OrgName, r.Deadline, int(r.DaysLeft))
				if err := s.notes.Emit(ctx, q, notifications.Notice{
					UserID: r.UserID, Kind: notifications.KindDeadlineReminder, Title: title, Body: body, Link: "/vacancies/" + r.VacancyID.String(),
				}); err != nil {
					return err
				}
				sent++
				return nil
			})
			if err != nil {
				failed++
				errs = append(errs, err)
			}
		}
		if len(rows) < s.cfg.Batch || failed > 0 {
			break
		}
	}
	return sent, errors.Join(errs...)
}
