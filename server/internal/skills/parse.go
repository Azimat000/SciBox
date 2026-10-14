package skills

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Skill — навык справочника.
type Skill struct {
	Kind    string
	Group   string
	Name    string
	Aliases []string
	// Line — строка файла (для сообщений об ошибках).
	Line int
}

// Parsed — разобранный файл.
type Parsed struct {
	Skills []Skill
}

// Пределы файла.
const (
	maxGroup   = 60
	maxAliases = 12
)

// ErrFormat — файл не похож на справочник навыков или в нём ошибка данных.
var ErrFormat = errors.New("skills: bad dictionary file")

// Parse разбирает файл справочника: CSV с разделителем «;», первая строка — заголовок
// `kind;group;name;aliases`, синонимы через «|». Пустые строки и строки, начинающиеся с «#», пропускаются.
// Любое нарушение (неизвестный вид, длина, повтор написания внутри вида) — ошибка с номером строки: молча терять
// навыки нельзя, справочник заменяется целиком.
func Parse(r io.Reader) (Parsed, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.Comment = '#'
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false

	head, err := cr.Read()
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: header: %w", ErrFormat, err)
	}
	if len(head) != 4 || strings.TrimPrefix(head[0], "\ufeff") != "kind" || head[1] != "group" || head[2] != "name" || head[3] != "aliases" {
		return Parsed{}, fmt.Errorf("%w: header must be kind;group;name;aliases", ErrFormat)
	}

	var out Parsed
	// owner: вид + ключ → строка, где написание встретилось первый раз.
	owner := map[string]int{}
	claim := func(kind, text string, line int) error {
		k := Key(text)
		id := kind + "\x00" + k
		if prev, dup := owner[id]; dup {
			return fmt.Errorf("%w: line %d: %q is already used on line %d", ErrFormat, line, text, prev)
		}
		owner[id] = line
		return nil
	}
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Parsed{}, fmt.Errorf("%w: %w", ErrFormat, err)
		}
		line, _ := cr.FieldPos(0)
		if len(row) != 4 {
			return Parsed{}, fmt.Errorf("%w: line %d: want 4 columns, got %d", ErrFormat, line, len(row))
		}
		s := Skill{Kind: row[0], Group: row[1], Name: row[2], Line: line}
		if !ValidKind(s.Kind) {
			return Parsed{}, fmt.Errorf("%w: line %d: unknown kind %q", ErrFormat, line, s.Kind)
		}
		if err := checkText(s.Group, maxGroup); err != nil {
			return Parsed{}, fmt.Errorf("%w: line %d: group: %w", ErrFormat, line, err)
		}
		if err := checkText(s.Name, MaxLen); err != nil {
			return Parsed{}, fmt.Errorf("%w: line %d: name: %w", ErrFormat, line, err)
		}
		if err := claim(s.Kind, s.Name, line); err != nil {
			return Parsed{}, err
		}
		if row[3] != "" {
			s.Aliases = strings.Split(row[3], "|")
			if len(s.Aliases) > maxAliases {
				return Parsed{}, fmt.Errorf("%w: line %d: more than %d aliases", ErrFormat, line, maxAliases)
			}
			for _, a := range s.Aliases {
				if err := checkText(a, MaxLen); err != nil {
					return Parsed{}, fmt.Errorf("%w: line %d: alias: %w", ErrFormat, line, err)
				}
				if err := claim(s.Kind, a, line); err != nil {
					return Parsed{}, err
				}
			}
		}
		out.Skills = append(out.Skills, s)
	}
	if len(out.Skills) == 0 {
		return Parsed{}, fmt.Errorf("%w: no skills", ErrFormat)
	}
	return out, nil
}

// checkText проверяет написание: непустое, без лишних пробелов по краям и внутри, без управляющих знаков, не длиннее max.
func checkText(s string, max int) error {
	switch {
	case s == "":
		return errors.New("empty")
	case !utf8.ValidString(s):
		return errors.New("not UTF-8")
	case utf8.RuneCountInString(s) > max:
		return fmt.Errorf("%q is longer than %d", s, max)
	case strings.Join(strings.Fields(s), " ") != s:
		return fmt.Errorf("%q has extra spaces", s)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%q has a control character", s)
		}
	}
	if Key(s) == "" {
		return fmt.Errorf("%q has no letters", s)
	}
	return nil
}
