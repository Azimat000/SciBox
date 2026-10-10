package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/jobs"
	"scibox/server/internal/mail"
	"scibox/server/internal/num"
	"scibox/server/internal/resilience"
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
	defaultBatch       = 20
	defaultLease       = 5 * time.Minute
	defaultAttempts    = 8
	defaultMaxAge      = 3 * 24 * time.Hour
	defaultSendTimeout = 15 * time.Second
	maxErrorRunes      = 500
	sentKeepFor        = 7 * 24 * time.Hour
	failedKeepFor      = 30 * 24 * time.Hour
	// markTimeout — сколько отводится на записи в базу после отправки письма: они не зависят от остановки сервера.
	markTimeout = 30 * time.Second
)

// Worker забирает письма из очереди и отправляет их настоящим отправителем.
//
// Устройство рассчитано на несколько копий и на остановку в любой момент (срез 25, D-168): письмо берётся в аренду, а
// вернуться в очередь оно может тремя путями: аренда истекла (копия упала), копию остановили (письма, которые она ещё не
// успела отправить, возвращаются сразу) или сбой самой почтовой службы (попытка письма не засчитывается).
type Worker struct {
	q      *dbgen.Queries
	mailer mail.Sender
	logger *slog.Logger
	now    func() time.Time
	// Batch — сколько писем брать за один проход.
	Batch int
	// Lease — на сколько письмо «закреплено» за отправителем: если он упал, письмо вернётся в очередь по истечении срока.
	Lease time.Duration
	// MaxAttempts — после стольких неудач самого письма оно считается недоставленным и больше не повторяется.
	MaxAttempts int
	// MaxAge — письмо старше этого срока после неудачи считается недоставленным, сколько бы попыток ни оставалось
	// (сбои почтовой службы попыток не тратят, и без предела письмо ждало бы вечно).
	MaxAge time.Duration
	// SendTimeout — сколько отводится одному письму; нужен, чтобы не начинать отправку, если аренда кончится раньше.
	SendTimeout time.Duration
	// Backoff — пауза перед повтором после неудачной попытки с номером attempt (начиная с 1).
	Backoff func(attempt int) time.Duration
	// Breaker — предохранитель почтовой службы (T-24): серия её сбоев останавливает отправку, письма ждут в очереди.
	// nil — без предохранителя.
	Breaker *resilience.Breaker
}

// NewWorker собирает отправителя.
func NewWorker(db DB, sender mail.Sender, logger *slog.Logger) *Worker {
	return &Worker{
		q: dbgen.New(db), mailer: sender, logger: logger, now: func() time.Time { return time.Now().UTC() },
		Batch: defaultBatch, Lease: defaultLease, MaxAttempts: defaultAttempts, MaxAge: defaultMaxAge,
		SendTimeout: defaultSendTimeout, Backoff: DefaultBackoff,
	}
}

// DefaultBackoff — 1 минута, 5 минут, 15 минут, час, 3 часа, 6 часов, 12 часов.
func DefaultBackoff(attempt int) time.Duration {
	steps := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour}
	i := max(attempt-1, 0)
	return steps[min(i, len(steps)-1)]
}

// Stats — итог одного прохода.
type Stats struct {
	// Sent — отправлено; Retry — не вышло, повтор с паузой; Failed — недоставлено окончательно.
	Sent, Retry, Failed int
	// Deferred — сбой почтовой службы: попытка письма не засчитана, письмо ждёт.
	Deferred int
	// Released — взято, но не отправлено (остановка, пауза предохранителя, кончалась аренда): возвращено в очередь.
	Released int
	// Paused — проход не начат: предохранитель почтовой службы открыт.
	Paused bool
}

// Handled — сколько писем проход довёл до какого-то итога (по нему решается, не брать ли следующую пачку сразу).
func (s Stats) Handled() int { return s.Sent + s.Retry + s.Failed + s.Deferred }

// RunOnce берёт пачку готовых к отправке писем и отправляет каждое. Ошибка базы прерывает проход; ошибка отправки
// одного письма нет: оно будет повторено позже, остальные идут дальше.
//
// Остановка (jobs.Draining) не прерывает письмо на середине: отправляемое дописывается, а ещё не начатые возвращаются
// в очередь. Записи итогов идут в контексте, который остановка не отменяет.
func (w *Worker) RunOnce(ctx context.Context) (Stats, error) {
	if jobs.Draining(ctx) {
		return Stats{}, nil
	}
	if w.Breaker != nil && w.Breaker.RetryAfter() > 0 {
		return Stats{Paused: true}, nil
	}
	now := w.now()
	leaseUntil := now.Add(w.Lease)
	rows, err := w.q.ClaimOutbox(ctx, dbgen.ClaimOutboxParams{LeaseUntil: leaseUntil, Now: now, Batch: num.Int32(w.Batch)})
	if err != nil {
		return Stats{}, fmt.Errorf("notifications: claim outbox: %w", err)
	}
	mark, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()

	var st Stats
	for i, r := range rows {
		// Не начинаем письмо, если сервер останавливают или аренда кончится раньше, чем оно успеет уйти: иначе другая
		// копия взяла бы его, пока мы ещё отправляем.
		if jobs.Draining(ctx) || (w.Lease > w.SendTimeout && w.now().Add(w.SendTimeout).After(leaseUntil)) {
			return w.release(mark, st, rows[i:], leaseUntil)
		}
		sendErr := w.send(ctx, r)
		done := w.now()
		if sendErr != nil && (resilience.Rejected(sendErr) || ctx.Err() != nil) {
			// Предохранитель открылся посреди пачки или отправку оборвала остановка: письмо не виновато.
			return w.release(mark, st, rows[i:], leaseUntil)
		}
		if err := w.record(mark, r, sendErr, done, &st); err != nil {
			return st, err
		}
	}
	return st, nil
}

// send отправляет письмо через предохранитель. Отклонённое предохранителем письмо возвращает его ошибку без обращения к службе.
func (w *Worker) send(ctx context.Context, r dbgen.ClaimOutboxRow) error {
	msg := mail.Message{To: r.ToEmail, Subject: r.Subject, Body: r.Body}
	if w.Breaker == nil {
		return w.mailer.Send(ctx, msg)
	}
	done, err := w.Breaker.Allow()
	if err != nil {
		return err
	}
	err = w.mailer.Send(ctx, msg)
	before := w.Breaker.Trips()
	done(w.outcome(ctx, err))
	if w.Breaker.Trips() > before {
		w.logger.Warn("mail service keeps failing: sending is paused", "retry_after", w.Breaker.RetryAfter().Round(time.Second), "err", err)
	}
	return err
}

// outcome: сбой службы (а не письма) приближает открытие предохранителя; отказ по письму говорит, что служба жива.
func (w *Worker) outcome(ctx context.Context, err error) resilience.Outcome {
	switch {
	case err == nil:
		return resilience.Success
	case ctx.Err() != nil:
		return resilience.Neutral
	case mail.KindOf(err) == mail.KindService:
		return resilience.Failure
	default:
		return resilience.Success
	}
}

// record записывает итог отправки одного письма.
func (w *Worker) record(ctx context.Context, r dbgen.ClaimOutboxRow, sendErr error, done time.Time, st *Stats) error {
	if sendErr == nil {
		if err := w.q.MarkOutboxSent(ctx, dbgen.MarkOutboxSentParams{ID: r.ID, Now: done}); err != nil {
			return fmt.Errorf("notifications: mark sent: %w", err)
		}
		st.Sent++
		return nil
	}
	kind := mail.KindOf(sendErr)
	reason := ""
	switch {
	case kind == mail.KindPermanent:
		reason = "refused for good"
	case w.MaxAge > 0 && done.Sub(r.CreatedAt) > w.MaxAge:
		reason = "too old"
	case kind != mail.KindService && int(r.Attempts) >= w.MaxAttempts:
		reason = "attempts are over"
	}
	switch {
	case reason != "":
		w.logger.Error("mail given up", "id", r.ID, "attempts", r.Attempts, "reason", reason, "err", sendErr)
		if err := w.q.MarkOutboxFailed(ctx, dbgen.MarkOutboxFailedParams{ID: r.ID, Now: done, LastError: shorten(sendErr.Error())}); err != nil {
			return fmt.Errorf("notifications: mark failed: %w", err)
		}
		st.Failed++
	case kind == mail.KindService:
		// Вина службы, не письма: попытка возвращается, письмо ждёт обычную первую паузу.
		w.logger.Warn("mail service failed, the mail waits", "id", r.ID, "err", sendErr)
		if err := w.q.DeferOutbox(ctx, dbgen.DeferOutboxParams{ID: r.ID, NextAttemptAt: done.Add(w.Backoff(1)), LastError: shorten(sendErr.Error())}); err != nil {
			return fmt.Errorf("notifications: defer mail: %w", err)
		}
		st.Deferred++
	default:
		w.logger.Warn("mail will be retried", "id", r.ID, "attempt", r.Attempts, "err", sendErr)
		if err := w.q.MarkOutboxRetry(ctx, dbgen.MarkOutboxRetryParams{ID: r.ID, NextAttemptAt: done.Add(w.Backoff(int(r.Attempts))), LastError: shorten(sendErr.Error())}); err != nil {
			return fmt.Errorf("notifications: mark retry: %w", err)
		}
		st.Retry++
	}
	return nil
}

// release возвращает в очередь письма, которые взяты, но не отправлены: сразу и без траты попытки. Возвращаются только те,
// что всё ещё в аренде этого отправителя.
func (w *Worker) release(ctx context.Context, st Stats, rows []dbgen.ClaimOutboxRow, leaseUntil time.Time) (Stats, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	n, err := w.q.ReleaseOutbox(ctx, dbgen.ReleaseOutboxParams{Now: w.now(), Ids: ids, LeaseUntil: leaseUntil})
	if err != nil {
		return st, fmt.Errorf("notifications: release outbox: %w", err)
	}
	st.Released = int(n)
	return st, nil
}

// Drain гоняет RunOnce, пока пачки получаются полными: очередь больше пачки разбирается без пауз между пачками. Останавливается
// при остановке сервера, паузе предохранителя и первой ошибке базы.
func (w *Worker) Drain(ctx context.Context) (Stats, error) {
	var total Stats
	for {
		st, err := w.RunOnce(ctx)
		total.Sent += st.Sent
		total.Retry += st.Retry
		total.Failed += st.Failed
		total.Deferred += st.Deferred
		total.Released += st.Released
		total.Paused = st.Paused
		if err != nil || st.Paused || st.Released > 0 || st.Handled() < w.Batch {
			return total, err
		}
	}
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
	if err := w.q.DeleteOldOutbox(ctx, dbgen.DeleteOldOutboxParams{SentBefore: new(now.Add(-sentKeepFor)), FailedBefore: new(now.Add(-failedKeepFor))}); err != nil {
		return fmt.Errorf("notifications: cleanup outbox: %w", err)
	}
	return nil
}

// QueueStats — состояние очереди: для журнала и метрик.
type QueueStats struct {
	// Waiting — письма, которые ещё не отправлены и не отброшены; Retrying — из них те, что уже пробовали.
	Waiting, Retrying int64
	// Undelivered — окончательно не доставленные (хранятся 30 дней).
	Undelivered int64
	// OldestWaiting — сколько ждёт самое старое из ждущих; 0, если очередь пуста.
	OldestWaiting time.Duration
}

// QueueStats читает состояние очереди.
func (w *Worker) QueueStats(ctx context.Context) (QueueStats, error) {
	now := w.now()
	row, err := w.q.OutboxStats(ctx)
	if err != nil {
		return QueueStats{}, fmt.Errorf("notifications: outbox stats: %w", err)
	}
	qs := QueueStats{Waiting: row.Waiting, Retrying: row.Retrying, Undelivered: row.Undelivered}
	if row.Waiting > 0 {
		qs.OldestWaiting = max(now.Sub(row.OldestWaiting), 0)
	}
	return qs, nil
}
