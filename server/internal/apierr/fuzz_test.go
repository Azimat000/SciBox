package apierr

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type fuzzDoc struct {
	Name  string            `json:"name"`
	Items []string          `json:"items"`
	Extra map[string]any    `json:"extra"`
	Inner *struct{ N int }  `json:"inner"`
	Tags  map[string]string `json:"tags"`
}

// FuzzDecodeJSON: любое тело либо разобрано и тогда это один документ без остатка, либо отвергнуто статусом 4xx с
// телом ошибки; паники нет, лимит тела соблюдается.
func FuzzDecodeJSON(f *testing.F) {
	for _, s := range []string{
		`{"name":"x"}`, `{"name":"x"} {"name":"y"}`, `{"name":"x"}garbage`, `{"unknown":1}`, `[]`, `null`, ``, `{"name":`,
		strings.Repeat("[", 2000), `{"items":["a","b"],"inner":{"N":9223372036854775808}}`, "\xef\xbb\xbf{}",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var d fuzzDoc
		ok := DecodeJSON(w, r, &d, 1<<10)
		if ok {
			if len(body) > 1<<10 {
				t.Fatalf("accepted %d bytes over the limit", len(body))
			}
			// Принятое тело — ровно один документ: тот же разбор «целиком» его тоже принимает.
			var again fuzzDoc
			dec := json.NewDecoder(strings.NewReader(body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&again); err != nil || dec.More() {
				t.Fatalf("accepted a body that is not one document: %q (%v)", body, err)
			}
			return
		}
		if w.Code < 400 || w.Code > 499 {
			t.Fatalf("refused with status %d", w.Code)
		}
	})
}
