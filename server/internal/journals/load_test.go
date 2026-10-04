package journals

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.RunMain(m)) }

var day = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fixture — маленький справочник для тестов с базой.
var fixture = head +
	row(1, "101", "Nature", "00280836, 14764687", "Nature Research", "18,5", "Q1") +
	row(2, "102", "Physical Review B", "24699950, 24699969", "APS", "1,3", "Q1") +
	row(3, "103", "Physics of the Solid State", "10637834", "Pleiades", "0,3", "Q3") +
	row(4, "104", "Journal of Physics: Condensed Matter", "09538984", "IOP", "0,8", "Q2") +
	row(5, "105", "Physics", "20411723", "MDPI", "0,2", "-") +
	row(6, "106", "100% Physics_Journal", "0031899X", "Pub", "", "Q4")

// loadFixture заменяет справочник в базе пакета маленьким.
func loadFixture(t *testing.T, csv string) Result {
	t.Helper()
	res, err := Load(testkit.BG, testkit.Pool, Source{Data: []byte(csv), Downloaded: day}, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return res
}

func TestLoadReplacesTheCatalogOnce(t *testing.T) {
	res := loadFixture(t, fixture)
	if res.Journals != 6 || res.ISSNs != 8 || res.Skipped != 0 || res.Unchanged || !strings.HasPrefix(res.Edition, "SJR 2025 · ") {
		t.Fatalf("first load = %+v", res)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journals`); n != 6 {
		t.Fatalf("journals = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journal_issns`); n != 8 {
		t.Fatalf("issns = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journals WHERE quartile IS NULL AND id = 105`); n != 1 {
		t.Fatal("«-» must be stored as no quartile")
	}
	var title, url, edition string
	var checked time.Time
	if err := testkit.Pool.QueryRow(testkit.BG, `SELECT title, url, edition, checked_on FROM reference_sources WHERE catalog = 'journals'`).
		Scan(&title, &url, &edition, &checked); err != nil {
		t.Fatal(err)
	}
	if title != SourceTitle || url != SourceURL || edition != res.Edition || !checked.Equal(day) {
		t.Fatalf("source = %q %q %q %v", title, url, edition, checked)
	}

	// Тот же файл (и сжатым) второй раз базу не трогает.
	again, err := Load(testkit.BG, testkit.Pool, Source{Data: gzipped(t, fixture), Downloaded: day}, false)
	if err != nil || !again.Unchanged || again.Edition != res.Edition {
		t.Fatalf("same file again = %+v, %v", again, err)
	}

	// Другой файл заменяет справочник целиком.
	next := head + row(1, "201", "Only one", "00280836", "", "", "Q2")
	res2, err := Load(testkit.BG, testkit.Pool, Source{Data: []byte(next), Downloaded: day.AddDate(1, 0, 0)}, false)
	if err != nil || res2.Unchanged || res2.Edition == res.Edition {
		t.Fatalf("new file = %+v, %v", res2, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journals`); n != 1 {
		t.Fatalf("after replace journals = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journal_issns WHERE issn = '0028-0836' AND journal_id = 201`); n != 1 {
		t.Fatal("the ISSN must point to the new journal")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM reference_sources WHERE catalog = 'journals'`); n != 1 {
		t.Fatal("one source row")
	}
}

func TestLoadTheEmbeddedFile(t *testing.T) {
	res, err := Load(testkit.BG, testkit.Pool, Embedded(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Journals < 30000 || res.ISSNs < res.Journals {
		t.Fatalf("embedded = %+v", res)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journal_issns x JOIN journals j ON j.id = x.journal_id WHERE x.issn = '0028-0836' AND j.quartile = 1`); n != 1 {
		t.Fatal("Nature must be Q1")
	}
	again, err := Load(testkit.BG, testkit.Pool, Embedded(), false)
	if err != nil || !again.Unchanged {
		t.Fatalf("embedded again = %+v, %v", again, err)
	}
}

func TestLoadRefusesABrokenFile(t *testing.T) {
	loadFixture(t, fixture)
	for name, data := range map[string][]byte{
		"not csv":     []byte("hello"),
		"broken gzip": {0x1f, 0x8b, 1, 2, 3},
	} {
		if _, err := Load(testkit.BG, testkit.Pool, Source{Data: data, Downloaded: day}, true); !errors.Is(err, ErrFormat) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM journals`); n != 6 {
		t.Fatalf("a refused file must leave the catalog alone: %d", n)
	}
}

// COPY, который база не приняла, откатывает всю замену.
func TestStoreRollsBackOnCopyFailure(t *testing.T) {
	loadFixture(t, fixture)
	bad := []Parsed{
		{Year: 2025, Journals: []Journal{{ID: 1, Title: "", ISSNs: []string{"0028-0836"}}}},                                                     // CHECK на название
		{Year: 2025, Journals: []Journal{{ID: 1, Title: "A", ISSNs: []string{"0028-0836"}}, {ID: 2, Title: "B", ISSNs: []string{"0028-0836"}}}}, // ISSN дважды
	}
	for i, p := range bad {
		if err := store(testkit.BG, testkit.Pool, p, "bad", day); err == nil {
			t.Fatalf("case %d: store must fail", i)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM journals`); n != 6 {
			t.Fatalf("case %d: journals after a failed store = %d", i, n)
		}
	}
}

func TestLoadNeverSwallowsADatabaseFailure(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		_, _ = testkit.Pool.Exec(testkit.BG, `DELETE FROM reference_sources WHERE catalog = 'journals'`)
		return func(db testkit.DB) error {
			_, err := Load(testkit.BG, db, Source{Data: []byte(fixture), Downloaded: day}, false)
			return err
		}
	})
}
