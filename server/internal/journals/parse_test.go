package journals

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// head — заголовок настоящей выгрузки SCImago 2025 (journalrank.php?out=xls).
const head = "Rank;Sourceid;Title;Type;Issn;Publisher;Open Access;Open Access Diamond;SJR;SJR Best Quartile;H index;Total Docs. (2025);Total Docs. (3years);Total Refs.;Total Citations (3years);Citable Docs. (3years);Citations / Doc. (2years);Ref. / Doc.;%Female;Overton;Country;Region;Publisher;Coverage;Categories;Areas\n"

// row — строка выгрузки с нужными полями; остальные столбцы как в файле.
func row(rank int, id, title, issn, publisher, sjr, quartile string) string {
	return strings.Join([]string{
		itoa(rank), id, `"` + title + `"`, "journal", `"` + issn + `"`, `"` + publisher + `"`, "No", "No", sjr, quartile,
		"100", "10", "30", "400", "500", "30", "3,1", "40,0", "30,0", "0", "Russian Federation", "Eastern Europe",
		`"` + publisher + `"`, `"2000-2026"`, `"Physics (` + quartile + `)"`, `"Physics and Astronomy"`,
	}, ";") + "\n"
}

func itoa(n int) string {
	var b []byte
	if n == 0 {
		return "0"
	}
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func parse(t *testing.T, s string) Parsed {
	t.Helper()
	p, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestParseReadsTheSCImagoFormat(t *testing.T) {
	csv := "\uFEFF" + head +
		row(1, "28773", "Ca-A Cancer Journal for Clinicians", "15424863, 00079235", "John Wiley and Sons Inc", "104,065", "Q1") +
		row(2, "21100", "Journal of  Physics;\n Condensed Matter", "09538984", "IOP", "0,9", "Q2") +
		row(3, "30000", "Third", "00280836", "", "", "Q3") +
		row(4, "30001", "Fourth", "0031899x", "Pub", "abc", "Q4") +
		row(5, "30002", "New journal", "2041-1723", "Pub", "", "-") +
		row(6, "30003", "Without quartile column value", "1234-5679", "Pub", "", "") +
		"7;30004;\"Advanced Letters\";journal;\"28129709\";\"Association \"The Center\" Novi Sad\";Yes;No;0,437;Q2\n"
	p := parse(t, csv)
	if p.Year != 2025 || p.Skipped != 0 || len(p.Journals) != 7 {
		t.Fatalf("parsed = year %d, skipped %d, %d journals", p.Year, p.Skipped, len(p.Journals))
	}
	j := p.Journals[0]
	if j.ID != 28773 || j.Title != "Ca-A Cancer Journal for Clinicians" || j.Publisher != "John Wiley and Sons Inc" || j.Quartile != 1 {
		t.Fatalf("first journal = %+v", j)
	}
	if strings.Join(j.ISSNs, ",") != "1542-4863,0007-9235" {
		t.Fatalf("issns = %v", j.ISSNs)
	}
	if j.SJR == nil || *j.SJR < 104.06 || *j.SJR > 104.07 {
		t.Fatalf("sjr = %v", j.SJR)
	}
	if p.Journals[1].Title != "Journal of Physics; Condensed Matter" || p.Journals[1].Quartile != 2 {
		t.Fatalf("quoted title with ; and line break = %+v", p.Journals[1])
	}
	if p.Journals[2].SJR != nil || p.Journals[3].SJR != nil {
		t.Fatal("empty and broken SJR must be nil")
	}
	if p.Journals[3].Quartile != 4 || p.Journals[3].ISSNs[0] != "0031-899X" {
		t.Fatalf("fourth = %+v", p.Journals[3])
	}
	if p.Journals[6].Publisher != `Association "The Center" Novi Sad` || p.Journals[6].Quartile != 2 {
		t.Fatalf("unescaped quotes inside a quoted field = %+v", p.Journals[6])
	}
	if p.Journals[4].Quartile != 0 || p.Journals[5].Quartile != 0 {
		t.Fatal("«-» and empty quartile mean «no quartile»")
	}
}

func TestParseKeepsTheBestRankedOwnerOfAnISSN(t *testing.T) {
	csv := head +
		row(1, "1", "Top", "00280836, 00280836", "", "", "Q1") +
		row(2, "2", "Second", "00280836, 20411723", "", "", "Q2") +
		row(3, "3", "Only taken", "00280836", "", "", "Q3") +
		row(4, "4", "No issn", "-", "", "", "Q1") +
		row(5, "5", "Broken issn", "12345678", "", "", "Q1")
	p := parse(t, csv)
	if len(p.Journals) != 2 || p.Skipped != 3 {
		t.Fatalf("journals %d, skipped %d", len(p.Journals), p.Skipped)
	}
	if strings.Join(p.Journals[0].ISSNs, ",") != "0028-0836" || strings.Join(p.Journals[1].ISSNs, ",") != "2041-1723" {
		t.Fatalf("issns = %v / %v", p.Journals[0].ISSNs, p.Journals[1].ISSNs)
	}
}

func TestParseDropsAnOddPublisher(t *testing.T) {
	long := strings.Repeat("П", maxPublisher+1)
	p := parse(t, head+row(1, "1", "A", "00280836", long, "", "Q1")+row(2, "2", "B", "20411723", "Bad \xff", "", "Q1"))
	if p.Journals[0].Publisher != "" || p.Journals[1].Publisher != "" {
		t.Fatalf("publishers = %q, %q", p.Journals[0].Publisher, p.Journals[1].Publisher)
	}
}

func TestParseRefusesWhatIsNotTheFile(t *testing.T) {
	ok := row(1, "1", "A", "00280836", "", "", "Q1")
	cases := map[string]string{
		"empty":            "",
		"no year column":   strings.Replace(head, "Total Docs. (2025)", "Total Docs.", 1) + ok,
		"no quartile":      strings.Replace(head, "SJR Best Quartile", "Best", 1) + ok,
		"no title":         strings.Replace(head, ";Title;", ";Name;", 1) + ok,
		"no issn":          strings.Replace(head, ";Issn;", ";ISSN-L;", 1) + ok,
		"no id":            strings.Replace(head, ";Sourceid;", ";Id;", 1) + ok,
		"only header":      head,
		"only no-issn":     head + row(1, "1", "A", "-", "", "", "Q1"),
		"bad id":           head + row(1, "x1", "A", "00280836", "", "", "Q1"),
		"zero id":          head + row(1, "0", "A", "00280836", "", "", "Q1"),
		"repeated id":      head + ok + row(2, "1", "B", "20411723", "", "", "Q1"),
		"empty title":      head + row(1, "1", " ", "00280836", "", "", "Q1"),
		"long title":       head + row(1, "1", strings.Repeat("a", maxTitle+1), "00280836", "", "", "Q1"),
		"bad utf8 title":   head + row(1, "1", "\xff\xfe", "00280836", "", "", "Q1"),
		"strange quartile": head + row(1, "1", "A", "00280836", "", "", "Q5"),
		"broken quotes":    head + "1;1;\"A;journal\n",
		"short row":        head + "1;1\n",
	}
	for name, csv := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(csv)); !errors.Is(err, ErrFormat) {
				t.Fatalf("err = %v, want ErrFormat", err)
			}
		})
	}
}

type failingReader struct{ rest string }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.rest == "" {
		return 0, errors.New("disk is gone")
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

func TestParseReportsAReadFailure(t *testing.T) {
	if _, err := Parse(&failingReader{rest: head + row(1, "1", "A", "00280836", "", "", "Q1")}); !errors.Is(err, ErrFormat) {
		t.Fatalf("err = %v", err)
	}
}

// Настоящий файл, встроенный в сервер: формат не поменялся, известные журналы на месте.
func TestParseTheEmbeddedFile(t *testing.T) {
	raw, err := unpack(Embedded().Data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p.Year != 2025 || len(p.Journals) < 30000 || p.Skipped > 2000 {
		t.Fatalf("year %d, journals %d, skipped %d", p.Year, len(p.Journals), p.Skipped)
	}
	byISSN := map[string]Journal{}
	for _, j := range p.Journals {
		for _, issn := range j.ISSNs {
			byISSN[issn] = j
		}
	}
	nature, ok := byISSN["0028-0836"]
	if !ok || nature.Title != "Nature" || nature.Quartile != 1 {
		t.Fatalf("Nature = %+v (found %v)", nature, ok)
	}
}

func TestUnpack(t *testing.T) {
	plain := []byte("plain text")
	if got, err := unpack(plain); err != nil || string(got) != "plain text" {
		t.Fatalf("plain = %q, %v", got, err)
	}
	if _, err := unpack([]byte{0x1f, 0x8b, 0, 1, 2}); !errors.Is(err, ErrFormat) {
		t.Fatalf("broken header err = %v", err)
	}
	gz := gzipped(t, "hello")
	if _, err := unpack(gz[:len(gz)-6]); !errors.Is(err, ErrFormat) {
		t.Fatalf("cut gzip err = %v", err)
	}
}
