package skills

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
	// Data — файл справочника (CSV), можно сжатый gzip.
	Data []byte
	// Compiled — когда составлен этот выпуск файла.
	Compiled time.Time
}

// Сведения об источнике (D-211): справочник составлен командой проекта, внешнего источника и лицензионных условий нет.
const (
	SourceTitle = "Справочник навыков SciBox: общеупотребимые названия научных методов, приборов, программ и областей, общих умений; составлен командой проекта"
	SourceURL   = ""
)

//go:embed data/skills.csv
var embedded []byte

// embeddedCompiled — когда составлен встроенный файл (его описание в docs/REFERENCE.md).
var embeddedCompiled = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

// Embedded — справочник, встроенный в сервер.
func Embedded() Source { return Source{Data: embedded, Compiled: embeddedCompiled} }

// LoadResult — что сделала загрузка.
type LoadResult struct {
	Edition  string
	Skills   int
	Research int
	General  int
	Terms    int
	// Unchanged — этот же файл уже загружен, база не тронута.
	Unchanged bool
}

// edition — выпуск справочника: число навыков и отпечаток файла. По нему повторная загрузка того же файла ничего не делает.
func edition(n int, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%d навыков · %s", n, hex.EncodeToString(sum[:])[:12])
}

// unpack распаковывает gzip; несжатый файл отдаёт как есть.
func unpack(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %w", ErrFormat, err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %w", ErrFormat, err)
	}
	return out, nil
}

// Load заменяет справочник файлом src одной транзакцией. Если этот же файл уже загружен, ничего не делает
// (force — загрузить всё равно). Профили хранят навыки строками, поэтому замена справочника их не меняет.
func Load(ctx context.Context, db DB, src Source, force bool) (LoadResult, error) {
	raw, err := unpack(src.Data)
	if err != nil {
		return LoadResult{}, err
	}
	parsed, err := Parse(bytes.NewReader(raw))
	if err != nil {
		return LoadResult{}, err
	}
	res := LoadResult{Edition: edition(len(parsed.Skills), raw), Skills: len(parsed.Skills)}
	for _, s := range parsed.Skills {
		res.Terms += 1 + len(s.Aliases)
		if s.Kind == KindResearch {
			res.Research++
		} else {
			res.General++
		}
	}
	q := dbgen.New(db)
	current, err := q.SkillsEdition(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return LoadResult{}, fmt.Errorf("skills: read edition: %w", err)
	case current == res.Edition && !force:
		res.Unchanged = true
		return res, nil
	}
	if err := store(ctx, db, parsed, res.Edition, src.Compiled); err != nil {
		return LoadResult{}, err
	}
	return res, nil
}

// store пишет справочник: старые строки удаляются, новые идут через COPY, источник обновляется.
func store(ctx context.Context, db DB, p Parsed, ed string, compiled time.Time) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("skills: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	if err := q.ClearSkills(ctx); err != nil {
		return fmt.Errorf("skills: clear: %w", err)
	}
	skills := make([][]any, 0, len(p.Skills))
	terms := make([][]any, 0, len(p.Skills)*2)
	for i, s := range p.Skills {
		id := num.Int32(i + 1)
		skills = append(skills, []any{id, s.Kind, s.Group, s.Name})
		for j, text := range append([]string{s.Name}, s.Aliases...) {
			k := Key(text)
			terms = append(terms, []any{s.Kind, k, text, WordsText(k), id, j == 0})
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"skills"}, []string{"id", "kind", "grp", "name"}, pgx.CopyFromRows(skills)); err != nil {
		return fmt.Errorf("skills: copy skills: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"skill_terms"}, []string{"kind", "key", "term", "words", "skill_id", "is_name"}, pgx.CopyFromRows(terms)); err != nil {
		return fmt.Errorf("skills: copy terms: %w", err)
	}
	if err := q.SaveSkillsSource(ctx, dbgen.SaveSkillsSourceParams{Title: SourceTitle, Url: SourceURL, Edition: ed, CheckedOn: compiled}); err != nil {
		return fmt.Errorf("skills: save source: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("skills: commit: %w", err)
	}
	return nil
}
