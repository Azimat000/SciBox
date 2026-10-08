package journals

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
)

// DB — то, что пакету нужно от базы: запросы и транзакции (в транзакции ещё и COPY).
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Source — файл справочника и сведения о нём для reference_sources.
type Source struct {
	// Data — выгрузка SCImago (CSV), можно сжатую gzip.
	Data []byte
	// Downloaded — когда файл скачан с сайта SCImago.
	Downloaded time.Time
}

// Сведения об источнике (условия SCImago: некоммерческое использование со ссылкой на источник, D-126).
const (
	SourceTitle = "SCImago Journal & Country Rank (SJR): лучший квартиль журнала по предметным категориям"
	SourceURL   = "https://www.scimagojr.com/journalrank.php"
)

//go:embed data/scimagojr.csv.gz
var embedded []byte

// embeddedDownloaded — когда скачан встроенный файл (его выпуск записан в docs/REFERENCE.md).
var embeddedDownloaded = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

// Embedded — файл SCImago, встроенный в сервер.
func Embedded() Source { return Source{Data: embedded, Downloaded: embeddedDownloaded} }

// Result — что сделала загрузка.
type Result struct {
	Edition  string
	Journals int
	ISSNs    int
	Skipped  int
	// Unchanged — этот же файл уже загружен, база не тронута.
	Unchanged bool
}

// edition — выпуск справочника: год данных и отпечаток файла. По нему повторная загрузка того же файла ничего не делает.
func edition(year int, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("SJR %d · %s", year, hex.EncodeToString(sum[:])[:12])
}

// unpack распаковывает gzip; несжатый файл отдаёт как есть.
func unpack(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrFormat, err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrFormat, err)
	}
	return out, nil
}

// Load заменяет справочник журналов файлом src одной транзакцией. Если этот же файл уже загружен, ничего не делает
// (force — загрузить всё равно). Ссылки публикаций на журналы — это ISSN, поэтому замена справочника их не ломает.
func Load(ctx context.Context, db DB, src Source, force bool) (Result, error) {
	raw, err := unpack(src.Data)
	if err != nil {
		return Result{}, err
	}
	parsed, err := Parse(bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	res := Result{Edition: edition(parsed.Year, raw), Journals: len(parsed.Journals), Skipped: parsed.Skipped}
	for _, j := range parsed.Journals {
		res.ISSNs += len(j.ISSNs)
	}
	q := dbgen.New(db)
	current, err := q.JournalsEdition(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Result{}, fmt.Errorf("journals: read edition: %w", err)
	case current == res.Edition && !force:
		res.Unchanged = true
		return res, nil
	}
	if err := store(ctx, db, parsed, res.Edition, src.Downloaded); err != nil {
		return Result{}, err
	}
	return res, nil
}

// store пишет справочник: старые строки удаляются, новые идут через COPY, источник обновляется.
func store(ctx context.Context, db DB, p Parsed, ed string, downloaded time.Time) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("journals: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	if err := q.ClearJournals(ctx); err != nil {
		return fmt.Errorf("journals: clear: %w", err)
	}
	rows := make([][]any, 0, len(p.Journals))
	issns := make([][]any, 0, len(p.Journals)*2)
	for _, j := range p.Journals {
		var quartile *int16
		if j.Quartile > 0 {
			quartile = new(num.Int16(j.Quartile))
		}
		rows = append(rows, []any{j.ID, j.Title, j.Publisher, quartile, j.SJR, num.Int16(p.Year)})
		for i, issn := range j.ISSNs {
			issns = append(issns, []any{issn, j.ID, int16(i)})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"journals"}, []string{"id", "title", "publisher", "quartile", "sjr", "data_year"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("journals: copy journals: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"journal_issns"}, []string{"issn", "journal_id", "position"}, pgx.CopyFromRows(issns)); err != nil {
		return fmt.Errorf("journals: copy issns: %w", err)
	}
	if err := q.SaveJournalsSource(ctx, dbgen.SaveJournalsSourceParams{Title: SourceTitle, Url: SourceURL, Edition: ed, CheckedOn: downloaded}); err != nil {
		return fmt.Errorf("journals: save source: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("journals: commit: %w", err)
	}
	return nil
}
