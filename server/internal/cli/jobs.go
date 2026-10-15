package cli

import (
	"context"
	"log/slog"
	"time"

	"scibox/server/internal/jobs"
	"scibox/server/internal/retention"
)

// Расписание фоновых задач (срез 25). Задачи «на весь кластер» идут раз в срок на все копии сервера и воркера
// (расписание в таблице job_runs); задачи «в каждой копии» гоняет каждая копия по своему таймеру, а от дублей их
// защищает сама работа: письмо берётся в аренду, файл берётся с отметкой.
const (
	// outboxInterval — как часто отправитель смотрит в очередь писем.
	outboxInterval = 2 * time.Second
	// fileWorkInterval — как часто проверяются непроверенные файлы, переносятся старые, удаляются объекты из очереди.
	fileWorkInterval = 30 * time.Second
	// matchingInterval — как часто смотрим, не пора ли слать письма о новых вакансиях и напоминания о сроках.
	matchingInterval = 5 * time.Minute
	// reportInterval — как часто в журнал пишется состояние очереди писем.
	reportInterval = 5 * time.Minute
	// housekeepingInterval — как часто идёт уборка по срокам.
	housekeepingInterval = time.Hour
	// fileSweepInterval — как часто чистятся осиротевшие объекты хранилища; идёт по всему хранилищу, поэтому редко.
	fileSweepInterval = 6 * time.Hour
	// fileSweepLease — сколько отводится одному проходу чистки осиротевших; упавшая копия не держит её дольше.
	fileSweepLease = time.Hour
	// unreadMailInterval — как часто смотрим, не пора ли слать письма о непрочитанных сообщениях (срез 39, D-245).
	unreadMailInterval = time.Minute
	// chatEventsInterval — как часто чистится журнал событий чата; chatEventsKeep — сколько он хранится (D-243).
	chatEventsInterval = 5 * time.Minute
	chatEventsKeep     = 30 * time.Minute
	// staleQueue — с какого возраста самое старое письмо в очереди считается признаком неполадки.
	staleQueue = 15 * time.Minute
)

// jobs — все фоновые задачи сервера. Список один для `serve` и `worker`: какой процесс их исполняет, решает настройка.
func (s *services) jobs() []jobs.Job {
	cluster := func(name string, every time.Duration, run func(ctx context.Context) error) jobs.Job {
		return jobs.Job{Name: name, Every: every, Cluster: true, Run: run}
	}
	sweep := cluster("files.sweep", fileSweepInterval, func(ctx context.Context) error {
		n, err := s.fileWork.Sweep(ctx)
		if n > 0 {
			s.logger.Info("orphan objects removed", "count", n)
		}
		return err
	})
	sweep.Lease = fileSweepLease

	return []jobs.Job{
		// В каждой копии: письма уходят параллельно, аренда не даёт взять одно письмо дважды.
		{Name: "outbox.send", Every: outboxInterval, Run: func(ctx context.Context) error {
			_, err := s.outbox.Drain(ctx)
			return err
		}},
		{Name: "files.check", Every: fileWorkInterval, Run: func(ctx context.Context) error {
			rep, err := s.fileWork.Pass(ctx)
			if rep.Total() > 0 {
				s.logger.Info("file work pass", "deleted", rep.Deleted, "moved", rep.Moved, "clean", rep.Clean, "infected", rep.Infected, "deferred", rep.Deferred)
			}
			return err
		}},

		// На весь кластер.
		cluster("outbox.report", reportInterval, s.reportOutbox),
		cluster("outbox.cleanup", housekeepingInterval, s.outbox.Cleanup),
		cluster("accounts.cleanup", housekeepingInterval, s.accounts.Cleanup),
		cluster("orgs.cleanup", housekeepingInterval, s.organizations.Cleanup),
		cluster("rate-counters.cleanup", housekeepingInterval, func(ctx context.Context) error {
			return s.counters.Cleanup(ctx, s.now())
		}),
		cluster("retention", housekeepingInterval, func(ctx context.Context) error {
			rep, err := retention.NewService(s.pool, retention.DefaultConfig(), s.logger).Pass(ctx)
			if rep.Total() > 0 {
				s.logger.Info("retention pass", "read_notifications", rep.ReadNotifications, "unread_notifications", rep.UnreadNotifications,
					"audit_log", rep.AuditLog, "audit_addresses", rep.AuditAddresses, "unconfirmed_accounts", rep.UnconfirmedAccounts)
			}
			return err
		}),
		cluster("chat.unread_mail", unreadMailInterval, func(ctx context.Context) error {
			n, err := s.chat.SendUnreadMail(ctx)
			if n > 0 {
				s.logger.Info("unread message mail", "queued", n)
			}
			return err
		}),
		cluster("chat.events_cleanup", chatEventsInterval, func(ctx context.Context) error {
			n, err := s.chat.PruneEvents(ctx, chatEventsKeep, 2000, 50)
			if n > 0 {
				s.logger.Info("chat events pruned", "rows", n)
			}
			return err
		}),
		cluster("matching.digests", matchingInterval, func(ctx context.Context) error {
			n, err := s.matching.SendDigests(ctx)
			if n > 0 {
				s.logger.Info("saved search digests", "sent", n)
			}
			return err
		}),
		cluster("matching.reminders", matchingInterval, func(ctx context.Context) error {
			n, err := s.matching.SendReminders(ctx)
			if n > 0 {
				s.logger.Info("deadline reminders", "sent", n)
			}
			return err
		}),
		sweep,
	}
}

// reportOutbox пишет состояние очереди писем в журнал: тихо, пока всё в порядке, и предупреждением, когда есть
// недоставленные письма или самое старое ждёт слишком долго (метрики для тревог дадут то же самое в срезе 26).
func (s *services) reportOutbox(ctx context.Context) error {
	qs, err := s.outbox.QueueStats(ctx)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if qs.Undelivered > 0 || qs.OldestWaiting > staleQueue {
		level = slog.LevelWarn
	}
	s.logger.Log(ctx, level, "mail queue", "waiting", qs.Waiting, "retrying", qs.Retrying, "undelivered", qs.Undelivered,
		"oldest_waiting", qs.OldestWaiting.Round(time.Second))
	return nil
}
