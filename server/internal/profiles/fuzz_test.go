package profiles

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

// FuzzCatalogParams: параметры каталога из адреса не роняют разбор; принятое годится для проверки без паники.
func FuzzCatalogParams(f *testing.F) {
	for _, s := range []string{"", "q=иванов&field=1&open=1&h_min=5", "h_min=x", "limit=99999999999999999999&offset=-5", "title=a&title=b&degree=%00", "q12_min=4&sort=h"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		q, _ := url.ParseQuery(raw)
		r := httptest.NewRequest("GET", "/api/scientists", nil)
		r.URL.RawQuery = q.Encode()
		p, err := catalogParams(r)
		if err != nil {
			return
		}
		_ = p.validate()
	})
}
