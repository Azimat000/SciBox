package refdata

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testdb"
)

func pool(t *testing.T) *pgxpool.Pool { return testdb.New(t) }

func TestCatalogContents(t *testing.T) {
	cat, err := NewService(pool(t)).Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Science) != 5 || len(cat.Regions) != 89 {
		t.Fatalf("fields %d, regions %d", len(cat.Science), len(cat.Regions))
	}
	groups, specs := 0, 0
	seen := map[string]bool{}
	for _, f := range cat.Science {
		groups += len(f.Groups)
		for _, g := range f.Groups {
			if len(g.Specialties) == 0 || g.Code[:1] != f.Code {
				t.Errorf("group %s %q in field %s", g.Code, g.Name, f.Code)
			}
			for _, s := range g.Specialties {
				specs++
				if seen[s.Code] || !strings.HasPrefix(s.Code, g.Code+".") || strings.HasPrefix(s.Name, "Утратила") {
					t.Errorf("specialty %s %q in group %s", s.Code, s.Name, g.Code)
				}
				seen[s.Code] = true
			}
		}
	}
	if groups != 35 || specs != 350 {
		t.Errorf("groups %d, specialties %d", groups, specs)
	}
	// Числа в кодах идут по порядку (1.2.9 раньше 1.2.10), а не по строкам.
	last := cat.Science[1].Groups
	if last[len(last)-1].Code != "2.10" || last[1].Code != "2.2" {
		t.Errorf("group order: %v", last)
	}
	for _, code := range []string{"1.4.4", "5.11.1"} {
		if !seen[code] {
			t.Errorf("specialty %s is missing", code)
		}
	}
	if seen["1.3.14"] || seen["2.5.9"] {
		t.Error("repealed specialties must not be listed")
	}
	// Положения о должностях: четыре типа, у каждого есть позиции.
	types := map[string]int{}
	for _, p := range cat.Positions {
		types[p.Type]++
	}
	for _, typ := range []string{"research", "teaching", "early_career", "management"} {
		if types[typ] < 4 {
			t.Errorf("type %s has %d positions", typ, types[typ])
		}
	}
	if len(cat.Sources) != 3 {
		t.Fatalf("sources: %+v", cat.Sources)
	}
	for _, s := range cat.Sources {
		if !strings.HasPrefix(s.URL, "https://") || s.Edition == "" || s.CheckedOn != "2026-10-03" {
			t.Errorf("source %+v", s)
		}
	}
	codes := map[string]bool{}
	for _, r := range cat.Regions {
		if len(r.Code) != 2 || codes[r.Code] {
			t.Errorf("region %+v", r)
		}
		codes[r.Code] = true
	}
}

func TestHandler(t *testing.T) {
	h := NewHandler(NewService(pool(t)), slog.New(slog.DiscardHandler))
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/reference", nil))
	var cat Catalog
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &cat) != nil || len(cat.Regions) != 89 {
		t.Fatalf("%d %.200s", rec.Code, rec.Body.String())
	}
}

// Сбой базы в любом из шести запросов доходит до вызывающего и не раскрывается в ответе.
var errBoom = errors.New("boom")

// countingDB ломает failAt-й запрос; остальные идут в настоящую базу.
type countingDB struct {
	dbgen.DBTX
	failAt int
	calls  *int
}

func (c *countingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	*c.calls++
	if *c.calls == c.failAt {
		return nil, errBoom
	}
	return c.DBTX.Query(ctx, sql, args...)
}

func TestDatabaseFailures(t *testing.T) {
	p := pool(t)
	for n := 1; n <= 6; n++ {
		calls := 0
		svc := NewService(&countingDB{DBTX: p, failAt: n, calls: &calls})
		if _, err := svc.Catalog(context.Background()); !errors.Is(err, errBoom) {
			t.Errorf("failure at call %d was lost: %v", n, err)
		}
	}
	h := NewHandler(NewService(&countingDB{DBTX: p, failAt: 1, calls: new(int)}), slog.New(slog.DiscardHandler))
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/reference", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
