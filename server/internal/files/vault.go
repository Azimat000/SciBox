package files

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
)

// KeyPrefix — начало ключа каждого файла в хранилище. Дальше идёт случайный номер: ни имени файла, ни людей в ключе нет.
const KeyPrefix = "files/"

// scanTimeout — сколько ждём проверку одного файла сразу при загрузке. Не уложились — файл остаётся «на проверке»,
// и его проверит фоновая задача; человек не ждёт антивирус дольше этого.
const scanTimeout = 20 * time.Second

// scanPause — сколько после сбоя антивируса новые загрузки не ждут его, а сразу становятся «на проверке»: иначе каждый
// файл запроса ждал бы свои scanTimeout секунд. Дальше проверит фоновая задача (filework).
const scanPause = 30 * time.Second

// ScanStatus — где файл в проверке на вирусы. Строки лежат в базе, не менять.
type ScanStatus string

const (
	// ScanPending — ещё не проверен (антивирус был недоступен). Организация такой файл не получает.
	ScanPending ScanStatus = "pending"
	// ScanClean — проверен, чист.
	ScanClean ScanStatus = "clean"
	// ScanInfected — найден вирус; содержимое удалено из хранилища.
	ScanInfected ScanStatus = "infected"
)

// Available — файл можно выдавать людям.
func (s ScanStatus) Available() bool { return s == ScanClean }

// ErrInfected — в файле найден вирус.
var ErrInfected = errors.New("files: virus found")

// InfectedError — какой именно вирус; название сигнатуры предназначено журналу, не человеку.
type InfectedError struct{ Signature string }

func (e *InfectedError) Error() string { return "files: virus found: " + e.Signature }

// Is делает InfectedError видимым для errors.Is(err, ErrInfected).
func (e *InfectedError) Is(target error) bool { return target == ErrInfected }

// ErrStorage — хранилище не приняло или не отдало файл. Человеку это «попробуйте позже», не его ошибка.
var ErrStorage = errors.New("files: storage failure")

// Stored — файл, уже записанный в хранилище, но ещё не привязанный к отклику или письму.
// Тот, кто его получил, либо сохраняет строку в базе, либо отдаёт его на удаление (Vault.Discard).
type Stored struct {
	// Name — имя, как его назвал человек; перед показом проходит через CleanName.
	Name string
	Key  string
	Size int64
	Sum  [sha256.Size]byte
	Scan ScanStatus
	// ScannedAt — когда проверен; нулевое время у файла «на проверке».
	ScannedAt time.Time
}

// Vault — склад файлов: хранилище объектов и антивирус. Принимает загрузку потоком, не держа файл в памяти целиком.
type Vault struct {
	store blobstore.Store
	scan  antivirus.Scanner
	now   func() time.Time
	// scanDownUntil — до какого момента (UnixNano) антивирус считается лежащим после недавнего сбоя.
	scanDownUntil atomic.Int64
}

// NewVault собирает склад. now — часы (в тестах подставные).
func NewVault(store blobstore.Store, scan antivirus.Scanner, now func() time.Time) *Vault {
	return &Vault{store: store, scan: scan, now: now}
}

// Store отдаёт хранилище объектов (фоновым задачам, которые ходят по ключам).
func (v *Vault) Store() blobstore.Store { return v.store }

// Scanner отдаёт антивирус (фоновым задачам).
func (v *Vault) Scanner() antivirus.Scanner { return v.scan }

// Now — текущее время склада.
func (v *Vault) Now() time.Time { return v.now() }

// NewKey — ключ нового файла.
func NewKey() string { return KeyPrefix + uuid.NewString() }

// pdfStream читает загрузку: считает размер и контрольную сумму, не пускает дальше предела и запоминает,
// чем кончилось чтение самого источника (это важно отличать от сбоя хранилища).
type pdfStream struct {
	r        io.Reader
	sum      hash.Hash
	n        int64
	exceeded bool
	srcErr   error
}

func (p *pdfStream) Read(b []byte) (int, error) {
	if p.exceeded {
		return 0, ErrTooBig
	}
	// Читаем не больше, чем осталось до предела плюс один байт: он покажет, что файл длиннее.
	if room := MaxFileSize - p.n + 1; int64(len(b)) > room {
		b = b[:room]
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.n += int64(n)
		if p.n > MaxFileSize {
			p.exceeded = true
			return 0, ErrTooBig
		}
		p.sum.Write(b[:n])
	}
	if err != nil && !errors.Is(err, io.EOF) {
		p.srcErr = err
	}
	return n, err
}

// Put принимает файл потоком. Порядок такой, чтобы ничего лишнего не попало в хранилище:
//  1. по первым байтам проверяется, что это PDF (расширению и типу из запроса не верим) — до любой записи;
//  2. файл пишется в хранилище потоком, размер ограничен MaxFileSize на лету;
//  3. записанный файл проверяется антивирусом. Найден вирус — объект удаляется и возвращается *FileError с ErrInfected.
//     Антивирус недоступен — файл остаётся со статусом ScanPending и до проверки организации не выдаётся.
//
// Ошибки чтения r (обрыв, слишком большой запрос) возвращаются как есть; сбой хранилища — ErrStorage.
func (v *Vault) Put(ctx context.Context, name string, r io.Reader) (Stored, error) {
	fail := func(err error) (Stored, error) { return Stored{}, &FileError{Name: CleanName(name), Err: err} }
	br := bufio.NewReaderSize(r, headerWindow)
	head, perr := br.Peek(headerWindow)
	if perr != nil && !errors.Is(perr, io.EOF) && !errors.Is(perr, bufio.ErrBufferFull) {
		return Stored{}, perr
	}
	switch {
	case len(head) == 0:
		return fail(ErrEmpty)
	case !bytes.Contains(head, pdfMagic):
		return fail(ErrNotPDF)
	}

	src := &pdfStream{r: br, sum: sha256.New()}
	key := NewKey()
	if _, err := v.store.Put(ctx, key, src); err != nil {
		switch {
		case src.exceeded:
			return fail(ErrTooBig)
		case src.srcErr != nil:
			return Stored{}, src.srcErr
		case ctx.Err() != nil:
			return Stored{}, ctx.Err() // человек ушёл посреди загрузки: это не поломка хранилища
		}
		return Stored{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	st := Stored{Name: name, Key: key, Size: src.n, Scan: ScanPending}
	copy(st.Sum[:], src.sum.Sum(nil))

	verdict, err := v.checkNow(ctx, key)
	switch {
	case err == nil && verdict.Infected:
		// Заражённый объект удаляется сразу; если не вышло, его уберёт чистка осиротевших (его нет ни в одной строке базы).
		_ = v.store.Delete(context.WithoutCancel(ctx), key)
		return fail(&InfectedError{Signature: verdict.Signature})
	case err == nil:
		st.Scan, st.ScannedAt = ScanClean, v.now()
	}
	return st, nil
}

// checkNow проверяет только что записанный файл, но не стучится в антивирус, который только что не ответил.
func (v *Vault) checkNow(ctx context.Context, key string) (antivirus.Verdict, error) {
	if v.now().UnixNano() < v.scanDownUntil.Load() {
		return antivirus.Verdict{}, antivirus.ErrUnavailable
	}
	verdict, err := v.Check(ctx, key)
	// Пауза включается только настоящим сбоем антивируса: человек, оборвавший загрузку посреди проверки, не должен
	// отключать проверку при загрузке для всех остальных.
	if errors.Is(err, antivirus.ErrUnavailable) && ctx.Err() == nil {
		v.scanDownUntil.Store(v.now().Add(scanPause).UnixNano())
	}
	return verdict, err
}

// PutTrusted записывает файл, который собрал сам сервер (резюме из профиля): он чист по происхождению, антивирус не нужен.
func (v *Vault) PutTrusted(ctx context.Context, name string, data []byte) (Stored, error) {
	key := NewKey()
	if _, err := v.store.Put(ctx, key, bytes.NewReader(data)); err != nil {
		return Stored{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	return Stored{Name: name, Key: key, Size: int64(len(data)), Sum: sha256.Sum256(data), Scan: ScanClean, ScannedAt: v.now()}, nil
}

// Check читает объект из хранилища и пропускает через антивирус. Ошибка — проверить не удалось (файл не считается чистым).
func (v *Vault) Check(ctx context.Context, key string) (antivirus.Verdict, error) {
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	obj, err := v.store.Get(ctx, key)
	if err != nil {
		return antivirus.Verdict{}, fmt.Errorf("files: open for scan: %w", err)
	}
	defer obj.Close() //nolint:errcheck // закрытие объекта для чтения ничего не пишет
	return v.scan.Scan(ctx, obj)
}

// Open открывает файл для выдачи. Права и статус проверки решает вызывающий.
func (v *Vault) Open(ctx context.Context, key string) (blobstore.Object, error) {
	obj, err := v.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	return obj, nil
}

// Discard удаляет записанные, но не принятые файлы. Вызывается и после отмены запроса, поэтому не зависит от его контекста.
// Ошибка удаления не мешает: объект без строки в базе уберёт чистка осиротевших.
func (v *Vault) Discard(ctx context.Context, files ...Stored) {
	if len(files) == 0 {
		return
	}
	keys := make([]string, len(files))
	for i, f := range files {
		keys[i] = f.Key
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_ = v.store.Delete(ctx, keys...)
}
