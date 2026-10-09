package erasure_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/testkit"
)

// Карта персональных данных в docs/SECURITY.md описывает каждую таблицу базы (T-19, 152-ФЗ). Новая таблица без строки в
// карте роняет тест: сначала решаем, есть ли в ней персональные данные, как долго они хранятся и чем удаляются.
func TestPersonalDataMapCoversEveryTable(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(doc), "## Карта персональных данных")
	if !ok {
		t.Fatal("the map section is missing from docs/SECURITY.md")
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|(.*)$")
	mapped := map[string]string{}
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		if _, dup := mapped[m[1]]; dup {
			t.Errorf("table %s is described twice", m[1])
		}
		mapped[m[1]] = m[2]
	}

	rows, err := testkit.Pool.Query(bg, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'goose_db_version' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		cells, ok := mapped[table]
		if !ok {
			t.Errorf("table %s has no row in the personal data map (docs/SECURITY.md)", table)
			continue
		}
		// У таблицы без ПДн так и написано; у остальных заполнены все остальные столбцы: чьи, кто видит, срок, удаление.
		parts := strings.Split(strings.Trim(strings.TrimSpace(cells), "|"), "|")
		if len(parts) != 5 {
			t.Errorf("table %s: want 5 more columns after the name, got %d", table, len(parts))
			continue
		}
		for i, p := range parts {
			if strings.TrimSpace(p) == "" {
				t.Errorf("table %s: column %d of the map is empty", table, i+2)
			}
		}
	}
	for name := range mapped {
		if !slices.Contains(tables, name) {
			t.Errorf("the map describes %s, which is not a table of the database", name)
		}
	}
}
