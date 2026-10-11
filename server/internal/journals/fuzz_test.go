package journals

import (
	"strings"
	"testing"
)

// FuzzParse: разбор выгрузки SCImago из чего угодно не падает; принятый файл содержит только допустимые ISSN и квартили.
func FuzzParse(f *testing.F) {
	head := "Rank;Sourceid;Title;Type;Issn;SJR;SJR Best Quartile;H index;Total Docs. (2025);Publisher\n"
	f.Add(head + "1;21100;\"Nature\";journal;\"00280836, 14764687\";\"14,5\";Q1;1000;800;Springer\n")
	f.Add(head + "1;1;\"A \"B\" C\";journal;;\"0\";-;0;0;\n")
	f.Add(head)
	f.Add("")
	f.Add("\ufeff" + head + "2;2;x;journal;\"1234567\";1;Q9;1;1;p\n")
	f.Fuzz(func(t *testing.T, in string) {
		parsed, err := Parse(strings.NewReader(in))
		if err != nil {
			return
		}
		for _, j := range parsed.Journals {
			if j.Quartile < 0 || j.Quartile > 4 {
				t.Fatalf("journal %d: quartile %d", j.ID, j.Quartile)
			}
			for _, i := range j.ISSNs {
				if len(i) != 9 || i[4] != '-' {
					t.Fatalf("journal %d: ISSN %q is not in canonical form", j.ID, i)
				}
			}
		}
	})
}
