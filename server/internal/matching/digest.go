package matching

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/num"
	"scibox/server/internal/vacancies"
)

// maxPasses — сколько пачек фоновый цикл разбирает за один проход: чтобы один сбой не крутил цикл бесконечно.
const maxPasses = 20

// Run — фоновая работа в процессе сервера: рассылка по сохранённым поискам и напоминания о сроках. Каждый проход
// самостоятелен, ошибки пишутся в журнал, цикл живёт до отмены ctx.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) pass(ctx context.Context) {
	if n, err := s.SendDigests(ctx); err != nil {
		s.log.Error("saved search digests failed", "sent", n, "err", err)
	}
	if n, err := s.SendReminders(ctx); err != nil {
		s.log.Error("deadline reminders failed", "sent", n, "err", err)
	}
}

// SendDigests делает один проход по сохранённым поискам, до которых дошла очередь, и возвращает, сколько уведомлений
// отправлено. Сбой одного поиска не мешает остальным; ошибки собираются в одну.
func (s *Service) SendDigests(ctx context.Context) (int, error) {
	sent := 0
	var errs []error
	for pass := 0; pass < maxPasses; pass++ {
		ids, err := s.q.ListDueSavedSearches(ctx, dbgen.ListDueSavedSearchesParams{Now: s.now(), Batch: num.Int32(s.cfg.Batch)})
		if err != nil {
			return sent, errors.Join(append(errs, fmt.Errorf("matching: list due searches: %w", err))...)
		}
		failed := 0
		for _, id := range ids {
			ok, err := s.digest(ctx, id)
			switch {
			case err != nil:
				failed++
				errs = append(errs, err)
			case ok:
				sent++
			}
		}
		// Неудавшиеся остаются в очереди и попадут в следующий проход: здесь их повторять незачем.
		if len(ids) < s.cfg.Batch || failed > 0 {
			break
		}
	}
	return sent, errors.Join(errs...)
}

// digest обрабатывает один поиск: находит вакансии, опубликованные после прошлой проверки, и отправляет сводку. Поиск и
// сообщение меняются одной транзакцией, поэтому письмо не уйдёт дважды и не потеряется. Второе значение: сообщение отправлено.
func (s *Service) digest(ctx context.Context, id uuid.UUID) (bool, error) {
	sent := false
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		now := s.now()
		row, err := q.LockSavedSearchForRun(ctx, dbgen.LockSavedSearchForRunParams{ID: id, Now: now})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // уже обработан другим проходом, удалён или выключен
		}
		if err != nil {
			return fmt.Errorf("matching: lock search: %w", err)
		}
		until := now.Add(-s.cfg.Settle)
		// Не двигаем границу назад, если часы сдвинулись.
		if until.Before(row.CheckedAt) {
			until = row.CheckedAt
		}
		var sentAt *time.Time
		res, err := s.findNew(ctx, row, until)
		if err != nil {
			return err
		}
		if res.Total > 0 {
			items := make([]digestItem, len(res.Items))
			for i, c := range res.Items {
				items[i] = digestItem{Title: c.Title, Org: c.Organization.Name, City: c.City, Deadline: parseDay(c.Deadline)}
			}
			title, body := digestNotice(row.Name, res.Total, items)
			err := s.notes.Emit(ctx, q, notifications.Notice{
				UserID: row.UserID, Kind: notifications.KindSavedSearch, Title: title, Body: body, Link: "/saved-searches/" + row.ID.String(),
			})
			if err != nil {
				return err
			}
			sentAt, sent = &now, true
		}
		if err := q.AdvanceSavedSearch(ctx, dbgen.AdvanceSavedSearchParams{
			ID: row.ID, CheckedAt: until, NextRunAt: NextRun(row.Frequency, now), SentAt: sentAt,
		}); err != nil {
			return fmt.Errorf("matching: advance search: %w", err)
		}
		return nil
	})
	return sent && err == nil, err
}

// findNew ищет вакансии, опубликованные в окне (checked_at, until], по условиям сохранённого поиска. Условия, которые уже
// нельзя разобрать (например, значение убрали из справочника), дают «ничего нового»: такой поиск не должен застрять
// навсегда. Любой другой сбой (например, база) возвращается ошибкой: окно не сдвигается, поиск повторится в следующий проход.
func (s *Service) findNew(ctx context.Context, row dbgen.SavedSearch, until time.Time) (vacancies.SearchResult, error) {
	values, err := url.ParseQuery(row.Query)
	if err != nil {
		s.log.Warn("saved search has unreadable query", "search", row.ID)
		return vacancies.SearchResult{}, nil //nolint:nilerr // намеренно: испорченный поиск даёт «ничего нового», иначе он застрянет навсегда (см. комментарий к функции)
	}
	p, err := vacancies.ParseSearchQuery(values)
	if err != nil {
		s.log.Warn("saved search has invalid query", "search", row.ID, "err", err)
		return vacancies.SearchResult{}, nil
	}
	after := row.CheckedAt
	p.Sort, p.Limit, p.Offset = vacancies.SortNew, s.cfg.MaxListed, 0
	p.PublishedAfter, p.PublishedUntil, p.NoFuzzy = &after, &until, true
	res, err := s.vac.Search(ctx, p)
	var verr *auth.ValidationError
	if errors.As(err, &verr) || errors.Is(err, vacancies.ErrNotFound) {
		s.log.Warn("saved search conditions are no longer valid", "search", row.ID, "err", err)
		return vacancies.SearchResult{}, nil
	}
	if err != nil {
		return vacancies.SearchResult{}, fmt.Errorf("matching: run saved search: %w", err)
	}
	return res, nil
}

// parseDay разбирает срок подачи вида 2026-11-14; пусто или неверно — нет срока.
func parseDay(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}
