package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebHandler(t *testing.T) {
	site := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>SciBox</title>")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"assets/fonts/x.woff2": {Data: []byte("font")},
	}
	h := NewRouter(Deps{Web: WebHandler(site), Logger: slog.New(slog.DiscardHandler)})
	cases := []struct {
		name, method, path string
		status             int
		body, cache        string
	}{
		{"home", "GET", "/", 200, "<title>SciBox</title>", "no-cache"},
		{"index by name", "GET", "/index.html", 200, "<title>SciBox</title>", "no-cache"},
		{"page of the site", "GET", "/vacancies/42", 200, "<title>SciBox</title>", "no-cache"},
		{"nested page", "GET", "/my/vacancies/7/edit", 200, "<title>SciBox</title>", "no-cache"},
		{"head of a page", "HEAD", "/profile", 200, "", "no-cache"},
		{"file in the root", "GET", "/favicon.svg", 200, "<svg/>", ""},
		{"hashed asset", "GET", "/assets/app-abc123.js", 200, "console.log(1)", "public, max-age=31536000, immutable"},
		{"missing asset", "GET", "/assets/app-old.js", 404, "404 page not found", ""},
		{"missing file with extension", "GET", "/robots.txt", 404, "404 page not found", ""},
		{"folder of assets", "GET", "/assets/fonts", 404, "404 page not found", ""},
		{"escape attempt", "GET", "/../../etc/passwd", 200, "<title>SciBox</title>", "no-cache"},
		{"post to a page", "POST", "/vacancies", 405, "method_not_allowed", ""},
		{"unknown api stays json", "GET", "/api/nope", 404, "not_found", "no-store"},
		{"bare api", "GET", "/api", 404, "not_found", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			body, _ := io.ReadAll(w.Body)
			if w.Code != tc.status || !strings.Contains(string(body), tc.body) {
				t.Fatalf("%s %s = %d %q", tc.method, tc.path, w.Code, body)
			}
			if tc.cache != "" && w.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("Cache-Control = %q, want %q", w.Header().Get("Cache-Control"), tc.cache)
			}
			if tc.status == 200 && w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("nosniff header missing")
			}
		})
	}
}

func TestWebHandlerWithoutBuild(t *testing.T) {
	h := WebHandler(fstest.MapFS{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
}

// Без сайта всё вне /api — JSON 404, как раньше.
func TestRouterWithoutWeb(t *testing.T) {
	w := httptest.NewRecorder()
	NewRouter(Deps{Logger: slog.New(slog.DiscardHandler)}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/vacancies", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "not_found") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}
