package filework

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/files"
	"scibox/server/internal/testkit"
)

// ---- проверка на вирусы ----

func TestPendingFileBecomesCleanOnlyByTheAnswerClean(t *testing.T) {
	e := newEnv(t)
	me := e.Applicant("Мария")
	app := e.application(me)
	id, key := e.pendingFile(app, "a.pdf", pdfData("honest"))

	// Антивирус недоступен: файл остаётся на проверке и не становится чистым.
	e.AV.Fault = func() error { return antivirus.ErrUnavailable }
	rep := e.pass()
	f := e.file(id)
	if rep.Deferred != 1 || rep.Clean != 0 || f.status != "pending" || f.scanned != nil || f.attempts != 1 || f.next == nil {
		t.Fatalf("report %+v, file %+v", rep, f)
	}
	if !contains(e.log.String(), "antivirus unavailable") {
		t.Errorf("the outage is not in the log:\n%s", e.log)
	}
	if !e.Store.Has(key) {
		t.Fatal("a file waiting for the check was lost")
	}

	// До времени повтора антивирус не трогают.
	scans := e.AV.Scans()
	e.AV.Fault = nil
	if rep := e.pass(); rep.Total() != 0 || e.AV.Scans() != scans {
		t.Fatalf("a file was taken before its time: %+v", rep)
	}

	// Время подошло, антивирус ожил: файл чист.
	e.clock.Advance(time.Minute)
	rep = e.pass()
	f = e.file(id)
	if rep.Clean != 1 || f.status != "clean" || f.scanned == nil || f.next != nil || f.attempts != 2 {
		t.Fatalf("report %+v, file %+v", rep, f)
	}
	// Чистый файл больше не проверяется.
	scans = e.AV.Scans()
	if rep := e.pass(); rep.Total() != 0 || e.AV.Scans() != scans {
		t.Fatalf("a clean file was scanned again: %+v", rep)
	}
}

func TestInfectedFileIsRemovedAndThePersonIsTold(t *testing.T) {
	e := newEnv(t)
	me, other := e.Applicant("Мария"), e.Applicant("Пётр")
	app := e.application(me)
	id, key := e.pendingFile(app, "invoice.pdf", infectedData())
	okID, okKey := e.pendingFile(app, "ok.pdf", pdfData("honest"))

	rep := e.pass()
	f := e.file(id)
	if rep.Infected != 1 || rep.Clean != 1 || f.status != "infected" || f.key != nil || f.sig == nil || *f.sig == "" || f.scanned == nil {
		t.Fatalf("report %+v, file %+v", rep, f)
	}
	if e.Store.Has(key) {
		t.Error("the infected object is still in the storage")
	}
	if got := e.file(okID); got.status != "clean" || !e.Store.Has(okKey) {
		t.Errorf("the honest neighbour suffered: %+v", got)
	}
	notes := notificationsOf(t, me, "file_rejected")
	if len(notes) != 1 || !contains(notes[0], "invoice.pdf") || !contains(notes[0], "/applications/"+app.String()) || contains(notes[0], "Eicar") {
		t.Errorf("notifications for the applicant: %q", notes)
	}
	if n := len(notificationsOf(t, other, "file_rejected")); n != 0 {
		t.Errorf("a stranger was notified: %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1 AND subject = 'Файл не прошёл проверку на вирусы'`, me.Email); n != 1 {
		t.Errorf("mails: %d", n)
	}
	if !contains(e.log.String(), "virus found") {
		t.Errorf("the virus is not in the log:\n%s", e.log)
	}
	// Заражённый больше не проверяется и не воскресает.
	if rep := e.pass(); rep.Total() != 0 {
		t.Errorf("second pass: %+v", rep)
	}
}

func TestInfectedReferenceLetterTellsTheApplicantToAskAgain(t *testing.T) {
	e := newEnv(t)
	me := e.Applicant("Мария")
	app := e.application(me)
	key := files.NewKey()
	if _, err := e.Store.Put(bg, key, bytes.NewReader(infectedData())); err != nil {
		t.Fatal(err)
	}
	var refID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `INSERT INTO reference_requests (application_id, name, email, relation, token_hash, status, created_at, expires_at, last_sent_at)
		VALUES ($1, 'Иванов', $2, 'руководитель', $3, 'received', now(), now() + interval '1 day', now()) RETURNING id`, app, "ref@example.ru", []byte(uuid.NewString())).Scan(&refID); err != nil {
		t.Fatal(err)
	}
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO application_files (application_id, reference_id, kind, name, size, storage_key, scan_status, created_at)
		VALUES ($1, $2, 'reference_letter', 'letter.pdf', 10, $3, 'pending', now())`, app, refID, key); err != nil {
		t.Fatal(err)
	}
	if rep := e.pass(); rep.Infected != 1 {
		t.Fatalf("report %+v", rep)
	}
	notes := notificationsOf(t, me, "file_rejected")
	if len(notes) != 1 || !contains(notes[0], "рекомендател") {
		t.Errorf("notifications: %q", notes)
	}
}

func TestScanFailuresNeverMakeAFileClean(t *testing.T) {
	cases := map[string]func(e *env, key string){
		"the antivirus answers nonsense": func(e *env, _ string) {
			e.AV.Fault = func() error { return errors.New("something odd") }
		},
		"the object is gone from the storage": func(e *env, key string) {
			if err := e.Store.Delete(bg, key); err != nil {
				e.T.Fatal(err)
			}
		},
		"the object cannot be read": func(e *env, _ string) {
			e.Store.Fault = func(op, _ string) error {
				if op == "get" {
					return errors.New("storage hiccup")
				}
				return nil
			}
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			app := e.application(e.Applicant("Мария"))
			id, key := e.pendingFile(app, "a.pdf", pdfData("x"))
			breakIt(e, key)
			rep := e.pass()
			f := e.file(id)
			if rep.Clean != 0 || rep.Infected != 0 || rep.Deferred != 1 || f.status != "pending" || f.scanned != nil {
				t.Fatalf("report %+v, file %+v", rep, f)
			}
			if f.next == nil || !f.next.After(e.clock.Now()) {
				t.Errorf("the next attempt is not in the future: %+v", f.next)
			}
		})
	}
	t.Run("a missing object is called by its name in the log", func(t *testing.T) {
		e := newEnv(t)
		app := e.application(e.Applicant("Мария"))
		_, key := e.pendingFile(app, "a.pdf", pdfData("x"))
		_ = e.Store.Delete(bg, key)
		e.pass()
		if !contains(e.log.String(), "missing from the storage") {
			t.Errorf("log:\n%s", e.log)
		}
	})
}

func TestRetryPauseGrowsAndIsCapped(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := retryAfter(int32(i + 1)); got != w {
			t.Errorf("after attempt %d: %v, want %v", i+1, got, w)
		}
	}
	if got := retryAfter(0); got != 30*time.Second {
		t.Errorf("attempt 0: %v", got)
	}
	if got := retryAfter(1 << 30); got != time.Hour {
		t.Errorf("huge attempt count: %v", got)
	}
}

func TestACrashedScanReturnsTheFileAfterTheLease(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	id, _ := e.pendingFile(app, "a.pdf", pdfData("x"))
	// Копия сервера взяла файл и упала, не записав итог.
	batch, err := e.svc.q.ClaimFilesToScan(bg, claimParams(e.clock.Now(), 5, e.svc.cfg.Lease))
	if err != nil || len(batch) != 1 || batch[0].ID != id {
		t.Fatalf("claim: %+v %v", batch, err)
	}
	if rep := e.pass(); rep.Total() != 0 {
		t.Fatalf("a leased file was taken by another copy: %+v", rep)
	}
	e.clock.Advance(e.svc.cfg.Lease + time.Second)
	if rep := e.pass(); rep.Clean != 1 {
		t.Fatalf("the file did not come back after the lease: %+v", rep)
	}
}

func TestTwoCopiesNeverScanOneFileTwice(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	const n = 40
	ids := make([]uuid.UUID, n)
	for i := range n {
		data := pdfData("f")
		if i%10 == 0 {
			data = infectedData()
		}
		ids[i], _ = e.pendingFile(app, "a.pdf", data)
	}
	var wg sync.WaitGroup
	var clean, infected, deferred int
	var mu sync.Mutex
	for range 4 {
		wg.Go(func() {
			rep, err := e.svc.Pass(bg)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			clean, infected, deferred = clean+rep.Clean, infected+rep.Infected, deferred+rep.Deferred
			mu.Unlock()
		})
	}
	wg.Wait()
	if clean != n-4 || infected != 4 || deferred != 0 {
		t.Errorf("clean %d, infected %d, deferred %d", clean, infected, deferred)
	}
	if got := e.AV.Scans(); got != n {
		t.Errorf("the antivirus was called %d times for %d files", got, n)
	}
	if got := testkit.Count(t, `SELECT count(*) FROM notifications WHERE kind = 'file_rejected'`); got != 4 {
		t.Errorf("notifications: %d", got)
	}
}

func TestRejectOfAChangedRowLeavesEverythingAlone(t *testing.T) {
	e := newEnv(t)
	me := e.Applicant("Мария")
	app := e.application(me)
	id, key := e.pendingFile(app, "a.pdf", infectedData())
	// Строку успели пометить чистой (или стереть) между проверкой и записью итога: ни пометки, ни удаления объекта.
	if _, err := testkit.Pool.Exec(bg, `UPDATE application_files SET scan_status = 'clean', scanned_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.svc.reject(bg, id, key, "Test-Signature"); ok || err != nil {
		t.Errorf("a changed row was rejected: %v %v", ok, err)
	}
	if !e.Store.Has(key) || e.file(id).status != "clean" {
		t.Error("a changed row or its object was touched")
	}
	if n := len(notificationsOf(t, me, "file_rejected")); n != 0 {
		t.Errorf("notified about a file that was not rejected: %d", n)
	}
}

func TestRejectKeepsGoingWhenTheObjectCannotBeDeleted(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	id, key := e.pendingFile(app, "a.pdf", infectedData())
	e.Store.Fault = func(op, _ string) error {
		if op == "delete" {
			return errors.New("storage hiccup")
		}
		return nil
	}
	rep := e.pass()
	// Строка помечена, человек уведомлён; объект осиротел, и его уберёт чистка.
	if rep.Infected != 1 || e.file(id).status != "infected" || e.file(id).key != nil {
		t.Fatalf("report %+v, file %+v", rep, e.file(id))
	}
	if !contains(e.log.String(), "orphan sweep will remove it") {
		t.Errorf("log:\n%s", e.log)
	}
	// Осиротевший объект остался; чистка осиротевших его удалит, когда он состарится.
	e.Store.Fault = nil
	if !e.Store.Has(key) {
		t.Fatal("the object disappeared although its deletion failed")
	}
}

// ---- очередь удаления ----

func TestQueuedObjectsAreDeletedInBatches(t *testing.T) {
	e := newEnv(t)
	var keys []string
	for range 8 { // пачка по 3: три полных прохода и неполный
		k := files.NewKey()
		if _, err := e.Store.Put(bg, k, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
		if _, err := testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	kept := files.NewKey()
	_, _ = e.Store.Put(bg, kept, strings.NewReader("x"))
	if rep := e.pass(); rep.Deleted != 8 {
		t.Fatalf("report %+v", rep)
	}
	for _, k := range keys {
		if e.Store.Has(k) {
			t.Errorf("%s is still in the storage", k)
		}
	}
	if !e.Store.Has(kept) {
		t.Error("an object that was not queued was deleted")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM blob_deletions`); n != 0 {
		t.Errorf("%d keys left in the queue", n)
	}
}

func TestQueuedKeysWaitWhileTheStorageIsDown(t *testing.T) {
	e := newEnv(t)
	k := files.NewKey()
	_, _ = e.Store.Put(bg, k, strings.NewReader("x"))
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k); err != nil {
		t.Fatal(err)
	}
	e.Store.Fault = func(op, _ string) error {
		if op == "delete" {
			return errors.New("storage down")
		}
		return nil
	}
	rep, err := e.svc.Pass(bg)
	if err == nil || rep.Deleted != 0 {
		t.Fatalf("report %+v, err %v: a failed deletion must be reported", rep, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM blob_deletions`); n != 1 || !e.Store.Has(k) {
		t.Fatalf("the key was struck from the queue before the object was deleted (%d keys)", n)
	}
	e.Store.Fault = nil
	if rep := e.pass(); rep.Deleted != 1 || e.Store.Has(k) {
		t.Fatalf("after recovery: %+v", rep)
	}
}

func TestTwoCopiesDeleteEachQueuedKeyOnce(t *testing.T) {
	e := newEnv(t)
	for range 30 {
		k := files.NewKey()
		_, _ = e.Store.Put(bg, k, strings.NewReader("x"))
		if _, err := testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var deleted int
	var mu sync.Mutex
	for range 4 {
		wg.Go(func() {
			rep, err := e.svc.Pass(bg)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			deleted += rep.Deleted
			mu.Unlock()
		})
	}
	wg.Wait()
	if deleted != 30 || e.Store.Count() != 0 {
		t.Errorf("deleted %d, left in the storage %d", deleted, e.Store.Count())
	}
}

// ---- перенос старых файлов ----

// legacyRows заводит строки, как они были до среза 23: содержимое в базе. Ограничение «содержимое в базу не пишется»
// на время вставки снимается (оно для того и стоит, чтобы такие строки не появлялись).
func (e *env) legacyRows(app uuid.UUID, datas ...[]byte) []uuid.UUID {
	e.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `ALTER TABLE application_files DROP CONSTRAINT IF EXISTS application_files_no_content`); err != nil {
		e.T.Fatal(err)
	}
	e.T.Cleanup(func() {
		_, _ = testkit.Pool.Exec(bg, `DELETE FROM application_files WHERE legacy_data IS NOT NULL`)
		_, _ = testkit.Pool.Exec(bg, `ALTER TABLE application_files DROP CONSTRAINT IF EXISTS application_files_no_content`)
		_, _ = testkit.Pool.Exec(bg, `ALTER TABLE application_files ADD CONSTRAINT application_files_no_content CHECK (legacy_data IS NULL) NOT VALID`)
	})
	ids := make([]uuid.UUID, len(datas))
	for i, d := range datas {
		err := testkit.Pool.QueryRow(bg, `INSERT INTO application_files (application_id, kind, name, size, legacy_data, created_at) VALUES ($1, 'attachment', $2, $3, $4, now()) RETURNING id`,
			app, "legacy.pdf", len(d), d).Scan(&ids[i])
		if err != nil {
			e.T.Fatal(err)
		}
	}
	return ids
}

func TestLegacyFilesMoveToTheStorageWithAChecksum(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	a, b := pdfData("legacy-a"), infectedData()
	ids := e.legacyRows(app, a, b)

	rep := e.pass()
	if rep.Moved != 2 {
		t.Fatalf("report %+v", rep)
	}
	for i, want := range [][]byte{a, b} {
		var key *string
		var sum, legacy []byte
		var status string
		if err := testkit.Pool.QueryRow(bg, `SELECT storage_key, sha256, legacy_data, scan_status FROM application_files WHERE id = $1`, ids[i]).Scan(&key, &sum, &legacy, &status); err != nil {
			t.Fatal(err)
		}
		if legacy != nil {
			t.Errorf("file %d: content is still in the database", i)
		}
		h := sha256.Sum256(want)
		if !bytes.Equal(sum, h[:]) {
			t.Errorf("file %d: checksum differs", i)
		}
		if i == 0 {
			if key == nil || status != "clean" {
				t.Fatalf("file %d: key %v status %s (an old file was never scanned: it must be scanned after the move)", i, key, status)
			}
			o, err := e.Store.Get(bg, *key)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(o)
			if !bytes.Equal(got, want) {
				t.Errorf("file %d: the stored copy differs", i)
			}
		} else if status != "infected" || key != nil {
			t.Errorf("an old infected file must be found after the move: key %v status %s", key, status)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_files WHERE legacy_data IS NOT NULL`); n != 0 {
		t.Errorf("%d files still have their content in the database", n)
	}
}

func TestLegacyMoveRefusesACorruptCopy(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	ids := e.legacyRows(app, pdfData("a"))
	// Хранилище отдаёт обратно не то, что приняло.
	e.svc.vault = files.NewVault(storeWrap{Store: e.Store, get: func(ctx context.Context, key string) (blobstore.Object, error) {
		o, err := e.Store.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		return tamper{o}, nil
	}}, e.AV, e.clock.Now)
	rep, err := e.svc.Pass(bg)
	if err == nil || !contains(err.Error(), "checksum mismatch") || rep.Moved != 0 {
		t.Fatalf("report %+v, err %v", rep, err)
	}
	var legacy []byte
	var key *string
	if err := testkit.Pool.QueryRow(bg, `SELECT legacy_data, storage_key FROM application_files WHERE id = $1`, ids[0]).Scan(&legacy, &key); err != nil {
		t.Fatal(err)
	}
	if legacy == nil || key != nil {
		t.Error("the row was switched to a copy that did not match")
	}
	if e.Store.Count() != 0 {
		t.Errorf("the bad copy stayed in the storage: %v", e.Store.Keys())
	}
}

type tamper struct{ blobstore.Object }

func (t tamper) Read(p []byte) (int, error) {
	n, err := t.Object.Read(p)
	if n > 0 {
		p[0] ^= 0xff
	}
	return n, err
}

func TestLegacyMoveFailures(t *testing.T) {
	boom := errors.New("storage broke")
	cases := map[string]func(e *env){
		"put fails": func(e *env) {
			e.svc.vault = files.NewVault(storeWrap{Store: e.Store, put: func(context.Context, string, io.Reader) (int64, error) { return 0, boom }}, e.AV, e.clock.Now)
		},
		"read back fails": func(e *env) {
			e.svc.vault = files.NewVault(storeWrap{Store: e.Store, get: func(context.Context, string) (blobstore.Object, error) { return nil, boom }}, e.AV, e.clock.Now)
		},
		"read back breaks in the middle": func(e *env) {
			e.svc.vault = files.NewVault(storeWrap{Store: e.Store, get: func(ctx context.Context, key string) (blobstore.Object, error) {
				o, err := e.Store.Get(ctx, key)
				return brokenObject{o, boom}, err
			}}, e.AV, e.clock.Now)
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			app := e.application(e.Applicant("Мария"))
			ids := e.legacyRows(app, pdfData("a"))
			breakIt(e)
			rep, err := e.svc.Pass(bg)
			if !errors.Is(err, boom) || rep.Moved != 0 {
				t.Fatalf("report %+v, err %v", rep, err)
			}
			var legacy []byte
			if err := testkit.Pool.QueryRow(bg, `SELECT legacy_data FROM application_files WHERE id = $1`, ids[0]).Scan(&legacy); err != nil || legacy == nil {
				t.Errorf("a file whose move failed lost its content: %v", err)
			}
			if e.Store.Count() != 0 {
				t.Errorf("objects left after a failed move: %v", e.Store.Keys())
			}
		})
	}
}

type brokenObject struct {
	blobstore.Object
	err error
}

func (b brokenObject) Read([]byte) (int, error) { return 0, b.err }

func TestLegacyMoveIsDoneOnceWhenCopiesRace(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	datas := make([][]byte, 12)
	for i := range datas {
		datas[i] = pdfData(uuid.NewString())
	}
	e.legacyRows(app, datas...)
	var wg sync.WaitGroup
	var moved int
	var mu sync.Mutex
	for range 4 {
		wg.Go(func() {
			rep, err := e.svc.Pass(bg)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			moved += rep.Moved
			mu.Unlock()
		})
	}
	wg.Wait()
	if moved != len(datas) || e.Store.Count() != len(datas) {
		t.Errorf("moved %d, objects %d, want %d of each", moved, e.Store.Count(), len(datas))
	}
}

func TestNoFileContentCanBeWrittenToTheDatabaseAgain(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	_, err := testkit.Pool.Exec(bg, `INSERT INTO application_files (application_id, kind, name, size, legacy_data, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'abc', now())`, app)
	if err == nil {
		t.Fatal("the database accepted file content again (D-157)")
	}
	// И остальные правила строки: чистый без даты, заражённый с ключом, без содержимого нигде, неверная сумма.
	for name, q := range map[string]string{
		"clean without a date":         `INSERT INTO application_files (application_id, kind, name, size, storage_key, scan_status, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'files/k1', 'clean', now())`,
		"infected that keeps its key":  `INSERT INTO application_files (application_id, kind, name, size, storage_key, scan_status, scanned_at, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'files/k2', 'infected', now(), now())`,
		"no content anywhere":          `INSERT INTO application_files (application_id, kind, name, size, scan_status, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'pending', now())`,
		"unknown status":               `INSERT INTO application_files (application_id, kind, name, size, storage_key, scan_status, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'files/k3', 'maybe', now())`,
		"checksum of the wrong length": `INSERT INTO application_files (application_id, kind, name, size, storage_key, sha256, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'files/k4', 'abc', now())`,
		"signature that is too long":   `INSERT INTO application_files (application_id, kind, name, size, scan_status, scan_signature, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'infected', repeat('x', 201), now())`,
	} {
		if _, err := testkit.Pool.Exec(bg, q, app); err == nil {
			t.Errorf("%s: the database accepted it", name)
		}
	}
	// Заражённая запись без содержимого допустима: так выглядит отвергнутый файл.
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO application_files (application_id, kind, name, size, scan_status, scanned_at, scan_signature, created_at) VALUES ($1, 'attachment', 'x.pdf', 3, 'infected', now(), 'Test', now())`, app); err != nil {
		t.Errorf("a rejected file record: %v", err)
	}
	// Одно содержимое — один ключ.
	e.row(app, "attachment", "a.pdf", "files/same", 1)
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO application_files (application_id, kind, name, size, storage_key, created_at) VALUES ($1, 'attachment', 'b.pdf', 1, 'files/same', now())`, app); err == nil {
		t.Error("two rows may not share one object")
	}
}

// ---- чистка осиротевших объектов ----

func (e *env) objectAged(key string, age time.Duration) {
	e.T.Helper()
	// Время записи объекта задаёт часы хранилища в памяти.
	e.Store.Now = func() time.Time { return e.clock.Now().Add(-age) }
	if _, err := e.Store.Put(bg, key, strings.NewReader("x")); err != nil {
		e.T.Fatal(err)
	}
}

func TestSweepRemovesOnlyOldObjectsWithoutARow(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	known, orphan, fresh, other := files.NewKey(), files.NewKey(), files.NewKey(), "backups/keep-me"
	e.objectAged(known, 72*time.Hour)
	e.row(app, "attachment", "a.pdf", known, 1)
	e.objectAged(orphan, 72*time.Hour)
	e.objectAged(fresh, time.Minute) // загрузка идёт прямо сейчас: строки ещё нет
	e.objectAged(other, 72*time.Hour)

	n, err := e.svc.Sweep(bg)
	if err != nil || n != 1 {
		t.Fatalf("swept %d, err %v", n, err)
	}
	for key, want := range map[string]bool{known: true, orphan: false, fresh: true, other: true} {
		if e.Store.Has(key) != want {
			t.Errorf("%s: present = %v, want %v", key, !want, want)
		}
	}
}

func TestSweepIsAlsoTheNetForBatchesBiggerThanOneListing(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	for range 501 { // больше одной проверки по 500 ключей
		k := files.NewKey()
		e.objectAged(k, 72*time.Hour)
		e.row(app, "attachment", "a.pdf", k, 1)
	}
	orphan := files.NewKey()
	e.objectAged(orphan, 72*time.Hour)
	n, err := e.svc.Sweep(bg)
	if err != nil || n != 1 || e.Store.Has(orphan) || e.Store.Count() != 501 {
		t.Fatalf("swept %d, err %v, objects %d", n, err, e.Store.Count())
	}
}

func TestSweepRefusesWhenThereAreSuspiciouslyManyOrphans(t *testing.T) {
	e := newEnv(t)
	// База восстановлена из старой копии: строк нет, а объекты лежат. Удалять нельзя ничего.
	for range 30 {
		e.objectAged(files.NewKey(), 72*time.Hour)
	}
	n, err := e.svc.Sweep(bg)
	if err == nil || !contains(err.Error(), "nothing deleted") || n != 0 || e.Store.Count() != 30 {
		t.Fatalf("swept %d, err %v, objects %d", n, err, e.Store.Count())
	}
}

func TestSweepFailures(t *testing.T) {
	boom := errors.New("storage broke")
	t.Run("listing fails", func(t *testing.T) {
		e := newEnv(t)
		e.Store.Fault = func(op, _ string) error {
			if op == "walk" {
				return boom
			}
			return nil
		}
		if _, err := e.svc.Sweep(bg); !errors.Is(err, boom) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("deleting fails", func(t *testing.T) {
		e := newEnv(t)
		e.objectAged(files.NewKey(), 72*time.Hour)
		e.Store.Fault = func(op, _ string) error {
			if op == "delete" {
				return boom
			}
			return nil
		}
		if n, err := e.svc.Sweep(bg); !errors.Is(err, boom) || n != 0 {
			t.Errorf("n = %d, err = %v", n, err)
		}
	})
	t.Run("the database fails", func(t *testing.T) {
		e := newEnv(t)
		e.objectAged(files.NewKey(), 72*time.Hour)
		db, _ := testkit.Faulty(testkit.Pool, 1)
		s := newService(db, e.Vault, e.notes, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), e.clock.Now)
		if _, err := s.Sweep(bg); !errors.Is(err, testkit.ErrFault) {
			t.Errorf("err = %v", err)
		}
		if e.Store.Count() != 1 {
			t.Error("an object was deleted although the database could not be asked")
		}
	})
	t.Run("deleting in several batches", func(t *testing.T) {
		e := newEnv(t)
		e.svc.cfg.MaxOrphans = 100
		for range 7 { // DeleteBatch = 3
			e.objectAged(files.NewKey(), 72*time.Hour)
		}
		if n, err := e.svc.Sweep(bg); err != nil || n != 7 || e.Store.Count() != 0 {
			t.Errorf("n = %d, err = %v, objects %d", n, err, e.Store.Count())
		}
	})
}

// ---- конфигурация, цикл, сбои базы ----

func TestConfigFallsBackToSafeValues(t *testing.T) {
	s := newService(testkit.Pool, nil, nil, Config{OrphanAge: time.Minute, MaxOrphanShare: 5}, slog.Default(), time.Now)
	def := DefaultConfig()
	if s.cfg != def {
		t.Errorf("cfg = %+v, want %+v (an orphan age under an hour must never be accepted: a running upload would be deleted)", s.cfg, def)
	}
	if NewService(testkit.Pool, nil, nil, Config{}, slog.Default()).cfg != def {
		t.Error("an empty config is not the default one")
	}
	if got := (Report{Deleted: 1, Moved: 2, Clean: 3, Infected: 4, Deferred: 5}).Total(); got != 15 {
		t.Errorf("total = %d", got)
	}
}

func TestPassKeepsGoingAfterOneKindFails(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	id, _ := e.pendingFile(app, "a.pdf", pdfData("x"))
	k := files.NewKey()
	_, _ = e.Store.Put(bg, k, strings.NewReader("x"))
	_, _ = testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k)
	e.Store.Fault = func(op, _ string) error {
		if op == "delete" {
			return errors.New("delete is down")
		}
		return nil
	}
	rep, err := e.svc.Pass(bg)
	if err == nil || !contains(err.Error(), "delete queued") {
		t.Fatalf("err = %v", err)
	}
	if rep.Clean != 1 || e.file(id).status != "clean" {
		t.Errorf("a failure of the deletion queue stopped the scan: %+v", rep)
	}
}

func TestRunWorksUntilCancelledAndSweepsOnTheFirstTurn(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	id, _ := e.pendingFile(app, "a.pdf", pdfData("x"))
	orphan := files.NewKey()
	e.objectAged(orphan, 72*time.Hour)
	e.Store.Now = nil
	e.svc.cfg.MaxOrphans = 10
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { e.svc.Run(ctx, 10*time.Millisecond, time.Hour); close(done) }()
	deadline := time.After(5 * time.Second)
	for e.file(id).status != "clean" || e.Store.Has(orphan) {
		select {
		case <-deadline:
			t.Fatalf("Run did not do the work: file %s, orphan present %v\n%s", e.file(id).status, e.Store.Has(orphan), e.log)
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if !contains(e.log.String(), "file work pass") || !contains(e.log.String(), "orphan objects removed") {
		t.Errorf("the work is not in the log:\n%s", e.log)
	}
}

func TestRunReportsFailuresAndSurvivesThem(t *testing.T) {
	e := newEnv(t)
	e.Store.Fault = func(op, _ string) error { return errors.New("storage is down") }
	k := files.NewKey()
	_, _ = testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { e.svc.Run(ctx, 10*time.Millisecond, time.Hour); close(done) }()
	deadline := time.After(5 * time.Second)
	for !contains(e.log.String(), "file work pass failed") || !contains(e.log.String(), "orphan sweep failed") {
		select {
		case <-deadline:
			t.Fatalf("failures are not in the log:\n%s", e.log)
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

func TestOnePassIsBounded(t *testing.T) {
	e := newEnv(t)
	e.svc.cfg.MaxBatches = 1
	app := e.application(e.Applicant("Мария"))
	for range 8 { // ScanBatch = 5
		e.pendingFile(app, "a.pdf", pdfData("x"))
	}
	if rep := e.pass(); rep.Clean != 5 {
		t.Fatalf("first pass: %+v", rep)
	}
	if rep := e.pass(); rep.Clean != 3 {
		t.Fatalf("second pass: %+v", rep)
	}
	for range 5 { // DeleteBatch = 3
		k := files.NewKey()
		_, _ = e.Store.Put(bg, k, strings.NewReader("x"))
		_, _ = testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k)
	}
	if rep := e.pass(); rep.Deleted != 3 {
		t.Fatalf("deletions are not bounded: %+v", rep)
	}
	if rep := e.pass(); rep.Deleted != 2 {
		t.Fatalf("the rest of the deletions: %+v", rep)
	}
	datas := make([][]byte, 12) // не больше 10 за проход
	for i := range datas {
		datas[i] = pdfData(uuid.NewString())
	}
	e.legacyRows(app, datas...)
	if rep := e.pass(); rep.Moved != 10 {
		t.Fatalf("the move is not bounded: %+v", rep)
	}
	if rep := e.pass(); rep.Moved != 2 {
		t.Fatalf("the rest of the move: %+v", rep)
	}
}

func TestPassStopsOnCancellation(t *testing.T) {
	e := newEnv(t)
	app := e.application(e.Applicant("Мария"))
	id, _ := e.pendingFile(app, "a.pdf", pdfData("x"))
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := e.svc.Pass(ctx); err == nil {
		t.Error("a cancelled pass reported success")
	}
	if e.file(id).status != "pending" {
		t.Error("a cancelled pass changed a file")
	}
}

func TestPassDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		e := newEnv(t)
		app := e.application(e.Applicant("Мария"))
		e.pendingFile(app, "a.pdf", pdfData("x"))
		e.pendingFile(app, "v.pdf", infectedData())
		e.legacyRows(app, pdfData("old"))
		k := files.NewKey()
		_, _ = e.Store.Put(bg, k, strings.NewReader("x"))
		_, _ = testkit.Pool.Exec(bg, `INSERT INTO blob_deletions (storage_key, queued_at) VALUES ($1, now())`, k)
		return func(db testkit.DB) error {
			s := newService(db, e.Vault, e.notes, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), e.clock.Now)
			_, err := s.Pass(bg)
			return err
		}
	})
}
