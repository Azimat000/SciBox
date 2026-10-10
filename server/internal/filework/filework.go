// Package filework — фоновая работа с файлами откликов (срез 23, T-21): проверка на вирусы тех, кого не удалось проверить
// при загрузке, перенос старых файлов из базы в хранилище, удаление объектов стёртых аккаунтов и чистка осиротевших
// объектов. Критичная зона (docs/TESTING.md): ошибка здесь пропускает непроверенный файл организации или удаляет
// чужой.
//
// Правила, на которых держится безопасность:
//   - «не знаем» не значит «чисто»: файл становится clean только по ответу антивируса «чисто»; любой сбой откладывает
//     файл с нарастающей паузой, а сам файл остаётся недоступным;
//   - заражённый файл сначала помечается в базе (и человеку уходит уведомление), потом удаляется из хранилища; если
//     удалить не вышло, объект без строки уберёт чистка осиротевших;
//   - перенос старого файла сверяется контрольной суммой после чтения обратно из хранилища; строка меняется только
//     после совпадения, содержимое в базе обнуляется тем же запросом;
//   - две копии сервера не берут одну работу: строки берутся под FOR UPDATE SKIP LOCKED, проверка арендуется;
//   - чистка осиротевших отказывается работать, если «сирот» подозрительно много (например, базу восстановили из копии).
package filework

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
)

// Config — настройки задачи.
type Config struct {
	// ScanBatch — сколько файлов берётся на проверку за раз.
	ScanBatch int32
	// Lease — на сколько файл «арендуется» на время проверки: упавшая копия сервера через это время отдаёт его другой.
	Lease time.Duration
	// MaxBatches — сколько пачек одного вида берёт один проход; остальное доберёт следующий.
	MaxBatches int
	// DeleteBatch — сколько ключей удаляется из хранилища за одну транзакцию.
	DeleteBatch int32
	// OrphanAge — насколько старым должен быть объект без строки в базе, чтобы считаться осиротевшим. Загрузка и
	// запись строки идут секунды; сутки — запас на любые сбои.
	OrphanAge time.Duration
	// MaxOrphans и MaxOrphanShare — предел «сирот» за одну чистку: больше — отказ (подозрение на восстановление базы).
	MaxOrphans     int
	MaxOrphanShare float64
}

// DefaultConfig — обычные настройки.
func DefaultConfig() Config {
	return Config{ScanBatch: 20, Lease: 5 * time.Minute, MaxBatches: 50, DeleteBatch: 200, OrphanAge: 24 * time.Hour, MaxOrphans: 20, MaxOrphanShare: 0.1}
}

// DB — то, что нужно от базы.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — фоновая работа с файлами.
type Service struct {
	db     DB
	q      *dbgen.Queries
	vault  *files.Vault
	notes  *notifications.Service
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
}

// NewService собирает сервис.
func NewService(db DB, vault *files.Vault, notes *notifications.Service, cfg Config, logger *slog.Logger) *Service {
	return newService(db, vault, notes, cfg, logger, time.Now)
}

func newService(db DB, vault *files.Vault, notes *notifications.Service, cfg Config, logger *slog.Logger, now func() time.Time) *Service {
	def := DefaultConfig()
	if cfg.ScanBatch <= 0 {
		cfg.ScanBatch = def.ScanBatch
	}
	if cfg.Lease <= 0 {
		cfg.Lease = def.Lease
	}
	if cfg.MaxBatches <= 0 {
		cfg.MaxBatches = def.MaxBatches
	}
	if cfg.DeleteBatch <= 0 {
		cfg.DeleteBatch = def.DeleteBatch
	}
	if cfg.OrphanAge < time.Hour {
		cfg.OrphanAge = def.OrphanAge // моложе часа — риск удалить загружаемое прямо сейчас
	}
	if cfg.MaxOrphans <= 0 {
		cfg.MaxOrphans = def.MaxOrphans
	}
	if cfg.MaxOrphanShare <= 0 || cfg.MaxOrphanShare > 1 {
		cfg.MaxOrphanShare = def.MaxOrphanShare
	}
	return &Service{db: db, q: dbgen.New(db), vault: vault, notes: notes, cfg: cfg, logger: logger, now: now}
}

// Report — что сделал проход.
type Report struct {
	Deleted  int // объектов удалено из хранилища по очереди
	Moved    int // старых файлов перенесено из базы в хранилище
	Clean    int // файлов признано чистыми
	Infected int // файлов признано заражёнными
	Deferred int // файлов, которые проверить не удалось (повтор позже)
}

// Total — сколько всего сделано.
func (r Report) Total() int { return r.Deleted + r.Moved + r.Clean + r.Infected + r.Deferred }

// Pass выполняет один проход: очередь удаления, перенос старых файлов, проверка. Ошибка одного вида работы не
// останавливает остальные; все ошибки возвращаются вместе.
func (s *Service) Pass(ctx context.Context) (Report, error) {
	var (
		rep  Report
		errs []error
		err  error
	)
	if rep.Deleted, err = s.deleteQueued(ctx); err != nil {
		errs = append(errs, fmt.Errorf("filework: delete queued: %w", err))
	}
	if rep.Moved, err = s.moveLegacy(ctx); err != nil {
		errs = append(errs, fmt.Errorf("filework: move legacy: %w", err))
	}
	if err = s.scanPending(ctx, &rep); err != nil {
		errs = append(errs, fmt.Errorf("filework: scan: %w", err))
	}
	return rep, errors.Join(errs...)
}

// ---- очередь удаления ----

// deleteQueued удаляет из хранилища объекты, поставленные в очередь стиранием аккаунтов. Ключ вычёркивается из очереди
// только после удаления: хранилище недоступно — ключи ждут следующего прохода.
func (s *Service) deleteQueued(ctx context.Context) (int, error) {
	total := 0
	for range s.cfg.MaxBatches {
		n, err := s.deleteBatch(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if n < int(s.cfg.DeleteBatch) {
			return total, nil
		}
	}
	return total, nil
}

func (s *Service) deleteBatch(ctx context.Context) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	keys, err := q.ClaimBlobDeletions(ctx, s.cfg.DeleteBatch)
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	if err := s.vault.Store().Delete(ctx, keys...); err != nil {
		return 0, err
	}
	if err := q.DoneBlobDeletions(ctx, keys); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(keys), nil
}

// ---- перенос старых файлов ----

// moveLegacy переносит файлы, чьё содержимое ещё лежит в базе, в хранилище. Один файл — одна транзакция под
// блокировкой его строки; ошибка останавливает перенос (следующий проход попробует снова), строка при этом не меняется.
func (s *Service) moveLegacy(ctx context.Context) (int, error) {
	moved := 0
	for range s.cfg.MaxBatches * 10 {
		ok, err := s.moveOne(ctx)
		if err != nil {
			return moved, err
		}
		if !ok {
			return moved, nil
		}
		moved++
	}
	return moved, nil
}

func (s *Service) moveOne(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	row, err := q.NextLegacyFile(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	key, sum := files.NewKey(), sha256.Sum256(row.LegacyData)
	if err := s.putAndVerify(ctx, key, row.LegacyData, sum); err != nil {
		return false, fmt.Errorf("file %s: %w", row.ID, err)
	}
	n, err := q.FinishLegacyFile(ctx, dbgen.FinishLegacyFileParams{ID: row.ID, StorageKey: &key, Sha256: sum[:]})
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, fmt.Errorf("file %s: row changed during the move", row.ID)
	}
	if err := tx.Commit(ctx); err != nil {
		// Объект записан, но строка не изменилась: он осиротел, его уберёт чистка.
		return false, err
	}
	return true, nil
}

// putAndVerify записывает объект и читает его обратно: размер и SHA-256 должны совпасть с исходными.
// При несовпадении объект удаляется, и перенос считается проваленным.
func (s *Service) putAndVerify(ctx context.Context, key string, data []byte, want [sha256.Size]byte) error {
	store := s.vault.Store()
	if _, err := store.Put(ctx, key, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("put: %w", err)
	}
	fail := func(err error) error {
		_ = store.Delete(context.WithoutCancel(ctx), key)
		return err
	}
	obj, err := store.Get(ctx, key)
	if err != nil {
		return fail(fmt.Errorf("read back: %w", err))
	}
	defer obj.Close() //nolint:errcheck // чтение закончено; закрытие ничего не пишет
	h := sha256.New()
	n, err := io.Copy(h, obj)
	if err != nil {
		return fail(fmt.Errorf("read back: %w", err))
	}
	if n != int64(len(data)) || !bytes.Equal(h.Sum(nil), want[:]) {
		return fail(errors.New("checksum mismatch after the copy"))
	}
	return nil
}

// ---- проверка на вирусы ----

// retryAfter — через сколько повторить проверку после неудачной попытки: 30 секунд, потом вдвое дольше, не больше часа.
func retryAfter(attempts int32) time.Duration {
	d := 30 * time.Second
	for i := int32(1); i < attempts && d < time.Hour; i++ {
		d *= 2
	}
	return min(d, time.Hour)
}

func (s *Service) scanPending(ctx context.Context, rep *Report) error {
	var errs []error
	for range s.cfg.MaxBatches {
		now := s.now()
		batch, err := s.q.ClaimFilesToScan(ctx, dbgen.ClaimFilesToScanParams{Now: now, Lim: s.cfg.ScanBatch, LeaseUntil: now.Add(s.cfg.Lease)})
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		for _, f := range batch {
			if err := ctx.Err(); err != nil {
				return errors.Join(errs...)
			}
			if err := s.scanOne(ctx, f.ID, f.StorageKey, f.ScanAttempts, rep); err != nil {
				errs = append(errs, fmt.Errorf("file %s: %w", f.ID, err))
			}
		}
		if len(batch) < int(s.cfg.ScanBatch) {
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

// scanOne проверяет один файл. Итог записан в базе; сбои самой проверки идут в журнал и в паузу до повтора, а ошибка
// возвращается только тогда, когда не удалось записать итог в базу: такой сбой не должен молча пропасть.
func (s *Service) scanOne(ctx context.Context, id uuid.UUID, key string, attempts int32, rep *Report) error {
	verdict, err := s.vault.Check(ctx, key)
	switch {
	case err != nil:
		rep.Deferred++
		next := s.now().Add(retryAfter(attempts))
		switch {
		case errors.Is(err, blobstore.ErrNotFound):
			// Строка есть, объекта нет: данные потеряны. Файл остаётся недоступным; человек должен знать.
			s.logger.Error("file object is missing from the storage", "file", id, "key", key)
		case errors.Is(err, antivirus.ErrUnavailable):
			s.logger.Warn("antivirus unavailable, file stays pending", "file", id, "attempts", attempts, "next", next)
		default:
			s.logger.Error("file scan failed", "file", id, "err", err)
		}
		return s.q.DeferFileScan(ctx, dbgen.DeferFileScanParams{ID: id, NextAt: next})
	case verdict.Infected:
		rejected, err := s.reject(ctx, id, key, verdict.Signature)
		if rejected {
			rep.Infected++
		}
		return err
	}
	n, err := s.q.MarkFileClean(ctx, dbgen.MarkFileCleanParams{ID: id, StorageKey: &key, Now: s.now()})
	if err != nil {
		return err
	}
	if n > 0 {
		rep.Clean++
	}
	return nil
}

// reject помечает файл заражённым, сообщает автору и удаляет объект. Первое значение — признан ли файл заражённым.
func (s *Service) reject(ctx context.Context, id uuid.UUID, key, signature string) (bool, error) {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.MarkFileInfected(ctx, dbgen.MarkFileInfectedParams{ID: id, StorageKey: &key, Signature: signature, Now: s.now()})
		if err != nil {
			return err
		}
		if n == 0 {
			return errRowGone
		}
		info, err := q.GetFileNotice(ctx, id)
		if err != nil {
			return err
		}
		return s.notes.Emit(ctx, q, notifications.Notice{
			UserID: info.UserID, Kind: "file_rejected", Title: "Файл не прошёл проверку на вирусы",
			Body: rejectedText(info.Name, info.Kind, info.VacancyTitle), Link: "/applications/" + info.ApplicationID.String(),
		})
	})
	if errors.Is(err, errRowGone) {
		return false, nil // строку успели стереть или изменить: объект уберёт очередь удаления или чистка
	}
	if err != nil {
		return false, err
	}
	s.logger.Warn("virus found in a stored file, file removed", "file", id, "signature", signature)
	if err := s.vault.Store().Delete(context.WithoutCancel(ctx), key); err != nil {
		s.logger.Error("infected object not deleted, the orphan sweep will remove it", "file", id, "err", err)
	}
	return true, nil
}

func rejectedText(name, kind, vacancy string) string {
	if files.Kind(kind) == files.KindReferenceLetter {
		return fmt.Sprintf("Рекомендательное письмо в файле «%s» для вакансии «%s» не прошло проверку на вирусы и удалено. Попросите рекомендателя написать ещё раз.", name, vacancy)
	}
	return fmt.Sprintf("Файл «%s» в отклике на вакансию «%s» не прошёл проверку на вирусы и удалён. Проверьте компьютер и откликнитесь заново с другим файлом.", name, vacancy)
}

var errRowGone = errors.New("filework: row is gone")

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(dbgen.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- чистка осиротевших объектов ----

// Sweep удаляет объекты, на которые нет ни одной строки в базе и которые старше OrphanAge: их оставили оборвавшиеся
// загрузки и отклонённые отклики. Если «сирот» подозрительно много, не удаляет ничего и возвращает ошибку:
// так выглядит база, восстановленная из старой копии, и тогда решать должен человек.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	cutoff := s.now().Add(-s.cfg.OrphanAge)
	var (
		listed  int
		orphans []string
		batch   []string
	)
	check := func() error {
		if len(batch) == 0 {
			return nil
		}
		known, err := s.q.ExistingFileKeys(ctx, batch)
		if err != nil {
			return err
		}
		have := make(map[string]struct{}, len(known))
		for _, k := range known {
			have[k] = struct{}{}
		}
		for _, k := range batch {
			if _, ok := have[k]; !ok {
				orphans = append(orphans, k)
			}
		}
		batch = batch[:0]
		return nil
	}
	err := s.vault.Store().Walk(ctx, files.KeyPrefix, func(in blobstore.Info) error {
		listed++
		if in.ModTime.After(cutoff) {
			return nil // слишком свежий: возможно, строка ещё записывается
		}
		if batch = append(batch, in.Key); len(batch) >= 500 {
			return check()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := check(); err != nil {
		return 0, err
	}
	if limit := max(s.cfg.MaxOrphans, int(float64(listed)*s.cfg.MaxOrphanShare)); len(orphans) > limit {
		return 0, fmt.Errorf("filework: %d of %d objects have no row in the database (limit %d): the database may have been restored from an old copy, nothing deleted", len(orphans), listed, limit)
	}
	for start := 0; start < len(orphans); start += int(s.cfg.DeleteBatch) {
		end := min(start+int(s.cfg.DeleteBatch), len(orphans))
		if err := s.vault.Store().Delete(ctx, orphans[start:end]...); err != nil {
			return start, err
		}
	}
	return len(orphans), nil
}
