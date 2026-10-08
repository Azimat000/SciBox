package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/mail"
	"scibox/server/internal/num"
)

// Queue кладёт письма в очередь outbox вместо немедленной отправки. Годится везде, где нужен mail.Sender:
// письмо переживёт падение сервера, а сбой почтового сервера не потеряет его (отправитель повторит).
type Queue struct {
	q   *dbgen.Queries
	now func() time.Time
}

// NewQueue собирает очередь поверх базы.
func NewQueue(db DB) *Queue {
	return &Queue{q: dbgen.New(db), now: func() time.Time { return time.Now().UTC() }}
}

// Send ставит письмо в очередь. Неверный адрес или тема отвергаются сразу, а не после восьми неудачных попыток.
func (q *Queue) Send(ctx context.Context, m mail.Message) error {
	return enqueue(ctx, q.q, m, q.now())
}

var _ mail.Sender = (*Queue)(nil)

// Параметры отправителя по умолчанию.
const (
	defaultBatch    = 20
	defaultLease    = 5 * time.Minute
	defaultAttempts = 8
	maxErrorRunes   = 500
	sentKeepFor     = 7 * 24 * time.Hour
	failedKeepFor   = 30 * 24 * time.Hour
)

// Worker забирает письма из очереди и отправляет их настоящим отправителем.
type Worker struct {
	q      *dbgen.Queries
	mailer mail.Sender
	logger *slog.Logger
	now    func() time.Time
	// Batch — сколько писем брать за один проход.
	Batch int
	// Lease — на сколько письмо «закреплено» за отправителем: если он упал, письмо вернётся в очередь по истечении срока.
	Lease time.Duration
	// MaxAttempts — после стольких неудач письмо считается недоставленным и больше не повторяется.
	MaxAttempts int
	// Backoff — пауза перед повтором после неудачной попытки с номером attempt (начиная с 1).
	Backoff func(attempt int) time.Duration
}

// NewWorker собирает отправителя.
func NewWorker(db DB, sender mail.Sender, logger *slog.Logger) *Worker {
	return &Worker{
		q: dbgen.New(db), mailer: sender, logger: logger, now: func() time.Time { return time.Now().UTC() },
		Batch: defaultBatch, Lease: defaultLease, MaxAttempts: defaultAttempts, Backoff: DefaultBackoff,
	}
}

// DefaultBackoff — 1 минута, 5 минут, 15 минут, час, 3 часа, 6 часов, 12 часов.
func DefaultBackoff(attempt int) time.Duration {
	steps := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour}
	i := max(attempt-1, 0)
	return steps[min(i, len(steps)-1)]
}

// Stats — итог одного прохода.
type Stats struct{ Sent, Retry, Failed int }

// RunOnce берёт пачку готовых к отправке писем и отправляет каждое. Ошибка базы прерывает проход; ошибка отправки
// одного письма нет: оно будет повторено позже, остальные идут дальше.
func (w *Worker) RunOnce(ctx context.Context) (Stats, error) {
	now := w.now()
	rows, err := w.q.ClaimOutbox(ctx, dbgen.ClaimOutboxParams{LeaseUntil: now.Add(w.Lease), Now: now, Batch: num.Int32(w.Batch)})
	if err != nil {
		return Stats{}, fmt.Errorf("notifications: claim outbox: %w", err)
	}
	var st Stats
	for _, r := range rows {
		sendErr := w.mailer.Send(ctx, mail.Message{To: r.ToEmail, Subject: r.Subject, Body: r.Body})
		done := w.now()
		switch {
		case sendErr == nil:
			if err := w.q.MarkOutboxSent(ctx, dbgen.MarkOutboxSentParams{ID: r.ID, Now: done}); err != nil {
				return st, fmt.Errorf("notifications: mark sent: %w", err)
			}
			st.Sent++
		case int(r.Attempts) >= w.MaxAttempts:
			w.logger.Error("mail given up", "id", r.ID, "attempts", r.Attempts, "err", sendErr)
			if err := w.q.MarkOutboxFailed(ctx, dbgen.MarkOutboxFailedParams{ID: r.ID, Now: done, LastError: shorten(sendErr.Error())}); err != nil {
				return st, fmt.Errorf("notifications: mark failed: %w", err)
			}
			st.Failed++
		default:
			w.logger.Warn("mail will be retried", "id", r.ID, "attempt", r.Attempts, "err", sendErr)
			if err := w.q.MarkOutboxRetry(ctx, dbgen.MarkOutboxRetryParams{ID: r.ID, NextAttemptAt: done.Add(w.Backoff(int(r.Attempts))), LastError: shorten(sendErr.Error())}); err != nil {
				return st, fmt.Errorf("notifications: mark retry: %w", err)
			}
			st.Retry++
		}
	}
	return st, nil
}

func shorten(s string) string {
	if utf8.RuneCountInString(s) <= maxErrorRunes {
		return s
	}
	return string([]rune(s)[:maxErrorRunes])
}

// Cleanup убирает старые отправленные и окончательно неудавшиеся письма.
func (w *Worker) Cleanup(ctx context.Context) error {
	now := w.now()
	if err := w.q.DeleteOldOutbox(ctx, dbgen.DeleteOldOutboxParams{SentBefore: ptrTime(now.Add(-sentKeepFor)), FailedBefore: ptrTime(now.Add(-failedKeepFor))}); err != nil {
		return fmt.Errorf("notifications: cleanup outbox: %w", err)
	}
	return nil
}

func ptrTime(t time.Time) *time.Time { return &t }

// Run гоняет RunOnce каждые interval и раз в час чистит очередь, пока не отменят ctx. Если в очереди было полно писем,
// следующий проход идёт сразу, не дожидаясь паузы.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	nextCleanup := w.now()
	for {
		for {
			st, err := w.RunOnce(ctx)
			if err != nil {
				if ctx.Err() == nil {
					w.logger.Error("outbox pass failed", "err", err)
				}
				break
			}
			if st.Sent+st.Retry+st.Failed < w.Batch {
				break
			}
		}
		if now := w.now(); !now.Before(nextCleanup) {
			if err := w.Cleanup(ctx); err != nil && ctx.Err() == nil {
				w.logger.Error("outbox cleanup failed", "err", err)
			}
			nextCleanup = now.Add(time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
