package journals

import (
	"context"
	"fmt"
	"scibox/server/internal/issn"
	"strings"
	"unicode/utf8"

	"scibox/server/internal/dbgen"
)

// Пределы поиска.
const (
	// MinQuery — короче поиск не запускается (слишком много совпадений).
	MinQuery = 2
	// MaxQuery — длиннее запрос обрезается.
	MaxQuery = 200
	// maxWords — больше слов в названии не учитываем.
	maxWords     = 8
	defaultLimit = 10
	maxLimit     = 20
)

// Found — журнал в выдаче поиска.
type Found struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Publisher string `json:"publisher"`
	// ISSN — какой ISSN ставить в публикацию: совпавший с запросом или первый у журнала.
	ISSN     string   `json:"issn"`
	ISSNs    []string `json:"issns"`
	Quartile *int     `json:"quartile"` // nil — у журнала нет квартиля
	Year     int      `json:"year"`
}

// Service — поиск по справочнику.
type Service struct {
	q *dbgen.Queries
}

// NewService собирает сервис.
func NewService(db dbgen.DBTX) *Service { return &Service{q: dbgen.New(db)} }

// escapeLike экранирует знаки шаблона ILIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Search ищет журнал по ISSN (если запрос похож на ISSN) или по словам названия: каждое слово должно встретиться
// в названии. Названия в справочнике SCImago английские (у российских журналов — название переводной версии).
// Слишком короткий запрос даёт пустой список.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]Found, error) {
	query = strings.Join(strings.Fields(query), " ")
	if utf8.RuneCountInString(query) > MaxQuery {
		query = string([]rune(query)[:MaxQuery])
	}
	if utf8.RuneCountInString(query) < MinQuery {
		return []Found{}, nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	arg := dbgen.SearchJournalsParams{Q: query, Patterns: []string{}, Prefix: escapeLike(query) + "%", RowLimit: int32(min(limit, maxLimit))}
	if n, ok := issn.Normalize(query); ok {
		arg.Issn = n
	} else {
		for i, w := range strings.Fields(query) {
			if i == maxWords {
				break
			}
			arg.Patterns = append(arg.Patterns, "%"+escapeLike(w)+"%")
		}
	}
	rows, err := s.q.SearchJournals(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("journals: search: %w", err)
	}
	out := make([]Found, 0, len(rows))
	for _, r := range rows {
		// Журнал без ISSN запрос не возвращает: у каждого найденного есть хотя бы один.
		f := Found{ID: r.ID, Title: r.Title, Publisher: r.Publisher, ISSNs: r.Issns, ISSN: r.Issns[0], Year: int(r.DataYear)}
		if arg.Issn != "" {
			f.ISSN = arg.Issn
		}
		if r.Quartile != nil {
			q := int(*r.Quartile)
			f.Quartile = &q
		}
		out = append(out, f)
	}
	return out, nil
}
