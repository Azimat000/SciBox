package vacancies

import (
	"net/url"
	"reflect"
	"scibox/server/internal/textsafe"
	"testing"
)

// FuzzSearchQuery: строка запроса поиска из адреса не роняет разбор и проверку; принятые условия записываются обратно
// в адрес и читаются так же (ничего не теряется и не придумывается), запись устойчива.
func FuzzSearchQuery(f *testing.F) {
	for _, s := range []string{
		"", "q=физика&field=1.1&region=77&sort=deadline", "level=1&level=x", "salary_min=999999999999999999999", "unit=zz",
		"limit=-1&offset=2147483648", "q=%00%ff&field=%27%3B--", "housing=true&competition=1&term=a&term=b",
		"q=" + string(make([]byte, 0)) + "a%20b%20c", "%zz=1", "deadline=2026-13-45",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		q, err := url.ParseQuery(raw)
		if err != nil && len(q) == 0 {
			return
		}
		p, err := ParseSearchQuery(q)
		if err != nil {
			return
		}
		if err := ValidateSearch(&p); err != nil {
			return
		}
		if !textsafe.Valid(p.Query) || !textsafe.Valid(p.OrgSlug) || !textsafe.Valid(p.Region) {
			t.Fatalf("accepted conditions hold text the database refuses: %+v", p)
		}
		enc := p.Encode()
		q2, err := url.ParseQuery(enc)
		if err != nil {
			t.Fatalf("Encode gave an unparseable query %q: %v", enc, err)
		}
		p2, err := ParseSearchQuery(q2)
		if err != nil {
			t.Fatalf("Encode(%q) = %q does not parse: %v", raw, enc, err)
		}
		if err := ValidateSearch(&p2); err != nil {
			t.Fatalf("Encode(%q) = %q does not validate: %v", raw, enc, err)
		}
		if enc2 := p2.Encode(); enc2 != enc {
			t.Fatalf("Encode is not stable: %q then %q", enc, enc2)
		}
		p.Limit, p.Offset, p2.Limit, p2.Offset = 0, 0, 0, 0
		p.OrgSlug, p2.OrgSlug, p.UnitID, p2.UnitID = "", "", nil, nil
		if !reflect.DeepEqual(p, p2) {
			t.Fatalf("round trip changed the conditions:\n%+v\n%+v", p, p2)
		}
	})
}
