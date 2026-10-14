package skills

import (
	"context"
	"fmt"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
)

// Пределы поиска.
const (
	defaultLimit = 8
	maxLimit     = 20
	// minFuzzy — короче запрос не ищется «по похожим словам» (слишком много случайных совпадений).
	minFuzzy = 4
)

// Found — подсказка.
type Found struct {
	// Name — каноническое написание: оно и попадает в профиль.
	Name string `json:"name"`
	// Group — подпись группы («Методы», «Языки»).
	Group string `json:"group"`
	// Via — синоним, по которому навык найден; пусто, если совпало само название.
	Via string `json:"via,omitempty"`
}

// SearchResult — ответ поиска.
type SearchResult struct {
	Items []Found `json:"items"`
	// Fuzzy — точных совпадений нет, показаны похожие написания (опечатки).
	Fuzzy bool `json:"fuzzy"`
}

// Service — поиск и приведение к каноническому написанию.
type Service struct {
	q *dbgen.Queries
}

// NewService собирает сервис.
func NewService(db dbgen.DBTX) *Service { return &Service{q: dbgen.New(db)} }

// Search ищет подсказки в справочнике вида kind. Ярусы: точное написание, начало написания, начало слов, словоформы;
// если ничего нет и запрос не короткий, то похожие написания (опечатки). Пустой запрос даёт пустой список.
func (s *Service) Search(ctx context.Context, kind, text string, limit int) (SearchResult, error) {
	if !ValidKind(kind) {
		return SearchResult{}, ErrKind
	}
	q := parseQuery(text)
	if q.key == "" {
		return SearchResult{Items: []Found{}}, nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	rows, err := s.q.SearchSkills(ctx, dbgen.SearchSkillsParams{
		Kind: kind, Key: q.key, Prefix: q.prefix, Words: nonNil(q.words), Tsq: q.tsq, RowLimit: num.Int32(limit),
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("skills: search: %w", err)
	}
	if len(rows) > 0 {
		out := make([]Found, 0, len(rows))
		for _, r := range rows {
			out = append(out, found(r.Name, r.Grp, r.Via, r.IsName))
		}
		return SearchResult{Items: out}, nil
	}
	if len([]rune(q.key)) < minFuzzy {
		return SearchResult{Items: []Found{}}, nil
	}
	fuzzy, err := s.q.SearchSkillsFuzzy(ctx, dbgen.SearchSkillsFuzzyParams{Kind: kind, Key: q.key, RowLimit: num.Int32(limit)})
	if err != nil {
		return SearchResult{}, fmt.Errorf("skills: search fuzzy: %w", err)
	}
	out := make([]Found, 0, len(fuzzy))
	for _, r := range fuzzy {
		out = append(out, found(r.Name, r.Grp, r.Via, r.IsName))
	}
	return SearchResult{Items: out, Fuzzy: len(out) > 0}, nil
}

func found(name, group, via string, isName bool) Found {
	f := Found{Name: name, Group: group}
	if !isName {
		f.Via = via
	}
	return f
}

// nonNil превращает nil в пустой срез: sqlc отдаёт драйверу text[] , а nil он передаёт как NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Canonical приводит навыки человека к каноническому написанию справочника вида kind: синонимы сводятся к названию
// («Excel» → «Microsoft Excel»), написание без различий регистра и «ё» берётся из справочника. Остальное
// остаётся как есть (свои навыки). Порядок человека сохраняется, повторы (по ключу Key) убираются; если
// справочник пуст (ещё не загружен), меняется только это.
func Canonical(ctx context.Context, q *dbgen.Queries, kind string, list []string) ([]string, error) {
	if len(list) == 0 || !ValidKind(kind) {
		return list, nil
	}
	keys := make([]string, 0, len(list))
	for _, v := range list {
		keys = append(keys, Key(v))
	}
	rows, err := q.CanonicalSkills(ctx, dbgen.CanonicalSkillsParams{Kind: kind, Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("skills: canonical: %w", err)
	}
	byKey := make(map[string]string, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r.Name
	}
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for i, v := range list {
		if name, ok := byKey[keys[i]]; ok {
			v = name
		}
		if k := Key(v); !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out, nil
}
