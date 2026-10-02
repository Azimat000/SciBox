// Package covercheck сверяет профиль покрытия Go с порогами из docs/TESTING.md.
//
// Правила лежат в файле coverage.conf, по одной на строку:
//
//	include <префикс пакета>        учитывать пакеты с этим префиксом
//	exclude <префикс пакета>        не учитывать (причина в комментарии)
//	min <процент>                   общий порог по учитываемым пакетам
//	critical <префикс пакета> <%>   порог критичной зоны
//
// Всё после # считается комментарием.
package covercheck

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Rules — разобранный файл порогов.
type Rules struct {
	Include  []string
	Exclude  []string
	Min      float64
	Critical []Zone
}

// Zone — критичная зона с собственным порогом.
type Zone struct {
	Prefix string
	Min    float64
}

// ParseRules читает файл порогов.
func ParseRules(r io.Reader) (Rules, error) {
	var rules Rules
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text, _, _ := strings.Cut(sc.Text(), "#")
		f := strings.Fields(text)
		if len(f) == 0 {
			continue
		}
		bad := func() (Rules, error) {
			return Rules{}, fmt.Errorf("line %d: bad rule %q", line, strings.TrimSpace(text))
		}
		switch {
		case f[0] == "include" && len(f) == 2:
			rules.Include = append(rules.Include, f[1])
		case f[0] == "exclude" && len(f) == 2:
			rules.Exclude = append(rules.Exclude, f[1])
		case f[0] == "min" && len(f) == 2:
			v, err := parsePercent(f[1])
			if err != nil {
				return bad()
			}
			rules.Min = v
		case f[0] == "critical" && len(f) == 3:
			v, err := parsePercent(f[2])
			if err != nil {
				return bad()
			}
			rules.Critical = append(rules.Critical, Zone{Prefix: f[1], Min: v})
		default:
			return bad()
		}
	}
	if err := sc.Err(); err != nil {
		return Rules{}, err
	}
	if len(rules.Include) == 0 {
		return Rules{}, fmt.Errorf("no include rules")
	}
	return rules, nil
}

func parsePercent(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || v > 100 {
		return 0, fmt.Errorf("bad percent %q", s)
	}
	return v, nil
}

// Counts — покрытые и все операторы.
type Counts struct {
	Covered int
	Total   int
}

// Percent возвращает процент покрытия; пустой набор считается покрытым полностью.
func (c Counts) Percent() float64 {
	if c.Total == 0 {
		return 100
	}
	return 100 * float64(c.Covered) / float64(c.Total)
}

func (c *Counts) add(o Counts) {
	c.Covered += o.Covered
	c.Total += o.Total
}

// ParseProfile читает профиль `go test -coverprofile` и возвращает счётчики по пакетам.
// Один и тот же блок может встретиться несколько раз (при -coverpkg); он считается
// покрытым, если хоть один тест его выполнил.
func ParseProfile(r io.Reader) (map[string]Counts, error) {
	type block struct {
		stmts   int
		covered bool
	}
	blocks := map[string]*block{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if first {
			first = false
			if !strings.HasPrefix(text, "mode:") {
				return nil, fmt.Errorf("profile: missing mode line")
			}
			continue
		}
		// file.go:1.2,3.4 <stmts> <count>
		f := strings.Fields(text)
		if len(f) != 3 || !strings.Contains(f[0], ":") {
			return nil, fmt.Errorf("profile: bad line %q", text)
		}
		stmts, err1 := strconv.Atoi(f[1])
		count, err2 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("profile: bad numbers in %q", text)
		}
		b := blocks[f[0]]
		if b == nil {
			b = &block{stmts: stmts}
			blocks[f[0]] = b
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	pkgs := map[string]Counts{}
	for key, b := range blocks {
		file, _, _ := strings.Cut(key, ":")
		pkg := path.Dir(file)
		c := pkgs[pkg]
		c.Total += b.stmts
		if b.covered {
			c.Covered += b.stmts
		}
		pkgs[pkg] = c
	}
	return pkgs, nil
}

// Report — итог проверки.
type Report struct {
	Packages map[string]Counts
	Total    Counts
	Zones    map[string]Counts
	Failures []string
}

// Check применяет правила к счётчикам пакетов.
func Check(rules Rules, pkgs map[string]Counts) Report {
	rep := Report{Packages: map[string]Counts{}, Zones: map[string]Counts{}}
	for pkg, c := range pkgs {
		if !hasPrefix(pkg, rules.Include) || hasPrefix(pkg, rules.Exclude) {
			continue
		}
		rep.Packages[pkg] = c
		rep.Total.add(c)
		for _, z := range rules.Critical {
			if hasPrefix(pkg, []string{z.Prefix}) {
				zc := rep.Zones[z.Prefix]
				zc.add(c)
				rep.Zones[z.Prefix] = zc
			}
		}
	}
	if p := rep.Total.Percent(); p < rules.Min {
		rep.Failures = append(rep.Failures, fmt.Sprintf("total %.1f%% < %.1f%%", p, rules.Min))
	}
	for _, z := range rules.Critical {
		zc, ok := rep.Zones[z.Prefix]
		if !ok {
			continue // зона объявлена заранее, кода в ней ещё нет
		}
		if p := zc.Percent(); p < z.Min {
			rep.Failures = append(rep.Failures, fmt.Sprintf("critical %s %.1f%% < %.1f%%", z.Prefix, p, z.Min))
		}
	}
	return rep
}

func hasPrefix(pkg string, prefixes []string) bool {
	for _, p := range prefixes {
		if pkg == p || strings.HasPrefix(pkg, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// Print выводит отчёт в читаемом виде.
func (rep Report) Print(w io.Writer, rules Rules) {
	names := make([]string, 0, len(rep.Packages))
	for n := range rep.Packages {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := rep.Packages[n]
		fmt.Fprintf(w, "  %-50s %6.1f%%  (%d/%d)\n", n, c.Percent(), c.Covered, c.Total)
	}
	fmt.Fprintf(w, "  %-50s %6.1f%%  (порог %.0f%%)\n", "ИТОГО", rep.Total.Percent(), rules.Min)
	for _, z := range rules.Critical {
		if zc, ok := rep.Zones[z.Prefix]; ok {
			fmt.Fprintf(w, "  %-50s %6.1f%%  (порог %.0f%%)\n", "критичная "+z.Prefix, zc.Percent(), z.Min)
		}
	}
	if len(rep.Failures) == 0 {
		fmt.Fprintln(w, "Покрытие сервера: OK")
		return
	}
	fmt.Fprintln(w, "Покрытие сервера НИЖЕ ПОРОГА:")
	for _, f := range rep.Failures {
		fmt.Fprintln(w, "  - "+f)
	}
}

// Run читает правила и профиль, печатает отчёт и возвращает ошибку при нарушении порогов.
func Run(rulesFile, profile io.Reader, out io.Writer) error {
	rules, err := ParseRules(rulesFile)
	if err != nil {
		return fmt.Errorf("rules: %w", err)
	}
	pkgs, err := ParseProfile(profile)
	if err != nil {
		return err
	}
	rep := Check(rules, pkgs)
	rep.Print(out, rules)
	if len(rep.Failures) > 0 {
		return fmt.Errorf("coverage below threshold")
	}
	return nil
}
