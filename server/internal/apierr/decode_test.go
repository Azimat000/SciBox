package apierr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name        string
		contentType string
		body        string
		wantOK      bool
		wantStatus  int
		wantCode    string
	}{
		{"ok", "application/json", `{"name":"x"}`, true, 200, ""},
		{"ok with charset", "application/json; charset=utf-8", `{"name":"x"}`, true, 200, ""},
		{"no content type", "", `{"name":"x"}`, false, http.StatusUnsupportedMediaType, CodeUnsupportedMedia},
		{"form", "application/x-www-form-urlencoded", `name=x`, false, http.StatusUnsupportedMediaType, CodeUnsupportedMedia},
		{"broken", "application/json", `{`, false, http.StatusBadRequest, CodeBadRequest},
		{"unknown field", "application/json", `{"name":"x","extra":1}`, false, http.StatusBadRequest, CodeBadRequest},
		{"too big", "application/json", `{"name":"` + strings.Repeat("я", 100) + `"}`, false, http.StatusBadRequest, CodeBadRequest},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/", strings.NewReader(c.body))
		if c.contentType != "" {
			req.Header.Set("Content-Type", c.contentType)
		}
		rec := httptest.NewRecorder()
		var dst body
		if ok := DecodeJSON(rec, req, &dst, 64); ok != c.wantOK {
			t.Errorf("%s: ok = %v", c.name, ok)
		}
		if !c.wantOK {
			if rec.Code != c.wantStatus || !strings.Contains(rec.Body.String(), c.wantCode) {
				t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
			}
		} else if dst.Name != "x" {
			t.Errorf("%s: decoded %+v", c.name, dst)
		}
	}
}
