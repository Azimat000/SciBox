package covercheck

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const rulesText = `
# comment
include scibox/server/internal/
exclude scibox/server/internal/dbgen   # generated
min 90
critical scibox/server/internal/auth 97
critical scibox/server/internal/files 97   # not written yet
`

func TestParseRules(t *testing.T) {
	r, err := ParseRules(strings.NewReader(rulesText))
	if err != nil {
		t.Fatal(err)
	}
	if r.Min != 90 || len(r.Include) != 1 || len(r.Exclude) != 1 || len(r.Critical) != 2 || r.Critical[0].Min != 97 {
		t.Fatalf("rules = %+v", r)
	}
}

func TestParseRulesErrors(t *testing.T) {
	cases := map[string]string{
		"unknown keyword":   "include x\nmax 90",
		"min not number":    "include x\nmin ninety",
		"min out of range":  "include x\nmin 101",
		"critical bad pct":  "include x\ncritical x -1",
		"critical no pct":   "include x\ncritical x",
		"no include":        "min 90",
		"include two parts": "include a b",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRules(strings.NewReader(text)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestParseRulesScannerError(t *testing.T) {
	long := "include " + strings.Repeat("x", 70*1024)
	if _, err := ParseRules(strings.NewReader(long)); err == nil {
		t.Fatal("want token too long error")
	}
}

const profileText = `mode: atomic
scibox/server/internal/auth/a.go:1.1,2.2 3 1
scibox/server/internal/auth/a.go:3.1,4.2 1 0
scibox/server/internal/auth/a.go:3.1,4.2 1 0
scibox/server/internal/web/w.go:1.1,2.2 4 0
scibox/server/internal/web/w.go:1.1,2.2 4 2
scibox/server/internal/dbgen/q.go:1.1,2.2 50 0
scibox/server/cmd/api/main.go:1.1,2.2 5 0

`

func TestParseProfileMergesDuplicateBlocks(t *testing.T) {
	pkgs, err := ParseProfile(strings.NewReader(profileText))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Counts{
		"scibox/server/internal/auth":  {Covered: 3, Total: 4},
		"scibox/server/internal/web":   {Covered: 4, Total: 4},
		"scibox/server/internal/dbgen": {Covered: 0, Total: 50},
		"scibox/server/cmd/api":        {Covered: 0, Total: 5},
	}
	if len(pkgs) != len(want) {
		t.Fatalf("pkgs = %+v", pkgs)
	}
	for k, v := range want {
		if pkgs[k] != v {
			t.Fatalf("%s = %+v, want %+v", k, pkgs[k], v)
		}
	}
}

func TestParseProfileErrors(t *testing.T) {
	cases := map[string]string{
		"no mode":      "x.go:1.1,2.2 1 1\n",
		"bad fields":   "mode: set\nx.go:1.1,2.2 1\n",
		"no colon":     "mode: set\nxgo 1 1\n",
		"bad numbers":  "mode: set\nx.go:1.1,2.2 a 1\n",
		"bad count":    "mode: set\nx.go:1.1,2.2 1 b\n",
		"line too big": "mode: set\n" + strings.Repeat("y", 2*1024*1024) + "\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProfile(strings.NewReader(text)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestCheck(t *testing.T) {
	rules, _ := ParseRules(strings.NewReader(rulesText))
	pkgs, _ := ParseProfile(strings.NewReader(profileText))
	rep := Check(rules, pkgs)

	if rep.Total != (Counts{Covered: 7, Total: 8}) {
		t.Fatalf("total = %+v (dbgen and cmd must be excluded)", rep.Total)
	}
	// 87.5% < 90 и auth 75% < 97; зона files пуста и не проверяется.
	if len(rep.Failures) != 2 || !strings.Contains(rep.Failures[0], "total") || !strings.Contains(rep.Failures[1], "auth") {
		t.Fatalf("failures = %v", rep.Failures)
	}
}

func TestCheckPasses(t *testing.T) {
	rules := Rules{Include: []string{"p"}, Min: 50, Critical: []Zone{{Prefix: "p/crit", Min: 100}}}
	rep := Check(rules, map[string]Counts{
		"p/a":         {Covered: 1, Total: 2},
		"p/crit":      {Covered: 2, Total: 2},
		"p/crit/sub":  {Covered: 0, Total: 0},
		"p/critter":   {Covered: 0, Total: 1}, // не входит в зона p/crit
		"other/thing": {Covered: 0, Total: 100},
	})
	if len(rep.Failures) != 0 {
		t.Fatalf("failures = %v", rep.Failures)
	}
	if rep.Zones["p/crit"] != (Counts{Covered: 2, Total: 2}) {
		t.Fatalf("zone = %+v", rep.Zones["p/crit"])
	}
}

func TestPercentOfEmpty(t *testing.T) {
	if (Counts{}).Percent() != 100 {
		t.Fatal("empty set must count as covered")
	}
}

func TestRun(t *testing.T) {
	var out bytes.Buffer
	err := Run(strings.NewReader(rulesText), strings.NewReader(profileText), &out)
	if err == nil || !strings.Contains(out.String(), "НИЖЕ ПОРОГА") || !strings.Contains(out.String(), "критичная scibox/server/internal/auth") {
		t.Fatalf("err = %v, out = %s", err, out.String())
	}

	out.Reset()
	good := "mode: set\nscibox/server/internal/web/w.go:1.1,2.2 4 1\n"
	if err := Run(strings.NewReader(rulesText), strings.NewReader(good), &out); err != nil || !strings.Contains(out.String(), "OK") {
		t.Fatalf("err = %v, out = %s", err, out.String())
	}

	if err := Run(strings.NewReader("bogus"), strings.NewReader(good), &out); err == nil || !strings.Contains(err.Error(), "rules") {
		t.Fatalf("bad rules: err = %v", err)
	}
	if err := Run(strings.NewReader(rulesText), strings.NewReader("garbage"), &out); err == nil {
		t.Fatal("bad profile: want error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestReadErrors(t *testing.T) {
	if _, err := ParseRules(errReader{}); err == nil {
		t.Fatal("rules: want read error")
	}
	if _, err := ParseProfile(errReader{}); err == nil {
		t.Fatal("profile: want read error")
	}
}
