package crossref

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeDOI(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"10.1038/nature12373", "10.1038/nature12373", true},
		{"  10.1038/NATURE12373  ", "10.1038/nature12373", true},
		{"https://doi.org/10.1038/nature12373", "10.1038/nature12373", true},
		{"HTTP://DX.DOI.ORG/10.1038/nature12373", "10.1038/nature12373", true},
		{"doi.org/10.1038/nature12373", "10.1038/nature12373", true},
		{"DOI: 10.1038/nature12373", "10.1038/nature12373", true},
		{"doi:10.1038/nature12373.", "10.1038/nature12373", true},
		{"10.1002/(SICI)1097-0258(19980815/30)17:15/16<1661::AID-SIM968>3.0.CO;2-2", "10.1002/(sici)1097-0258(19980815/30)17:15/16<1661::aid-sim968>3.0.co;2-2", true},
		{"10.12345678/x", "10.12345678/x", true},
		{"", "", false},
		{"nature12373", "", false},
		{"10.12/abc", "", false},         // слишком короткий префикс
		{"10.1234567890/abc", "", false}, // слишком длинный префикс
		{"10.1038/", "", false},
		{"10.1038/na ture", "", false}, // пробел внутри
		{"11.1038/abc", "", false},
		{"10.1038/a\x00b", "", false},
		{"10.1038/" + strings.Repeat("a", maxDOILen), "", false},
	}
	for _, tc := range tests {
		got, ok := NormalizeDOI(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeDOI(%q) = %q, %v; ожидали %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestEscapeDOI(t *testing.T) {
	tests := map[string]string{
		"10.1038/nature12373": "10.1038/nature12373",
		"10.1002/a#b":         "10.1002/a%23b",
		"10.1002/a?b":         "10.1002/a%3Fb",
		"10.1002/(x)<y>":      "10.1002/%28x%29%3Cy%3E", // скобки в процентной записи — то же самое для сервера
		"10.1002/a/b/c":       "10.1002/a/b/c",
		"10.1002/эй":          "10.1002/%D1%8D%D0%B9",
	}
	for in, want := range tests {
		if got := escapeDOI(in); got != want {
			t.Errorf("escapeDOI(%q) = %q, ожидали %q", in, got, want)
		}
	}
}

// fake — подставной Crossref: запоминает запросы, отвечает заданным.
type fake struct {
	mu     sync.Mutex
	paths  []string
	agents []string
	status int
	body   string
	delay  time.Duration
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.EscapedPath())
	f.agents = append(f.agents, r.Header.Get("User-Agent"))
	status, body, delay := f.status, f.body, f.delay
	f.mu.Unlock()
	time.Sleep(delay)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func newClient(t *testing.T, f *fake, mailto string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", mailto)
}

const fullBody = `{"status":"ok","message":{
  "DOI":"10.1038/NATURE12373","type":"journal-article",
  "title":["Nanometre-scale thermometry in a <i>living</i> cell &amp; more"],
  "container-title":["Nature"],"volume":"500","issue":"7460","page":"54-58","ISSN":["0028-0836"," 1476-4687 ",""],
  "author":[{"given":"G.","family":"Kucsko"},{"given":"Peter C.","family":"Maurer"},{"name":"Harvard Collaboration"},{"given":"","family":""},{"given":"Jean-Paul","family":"Sartre"}],
  "issued":{"date-parts":[[2013,8,1]]}
}}`

func TestLookupFull(t *testing.T) {
	f := &fake{status: 200, body: fullBody}
	c := newClient(t, f, "ops@example.ru")
	w, err := c.Lookup(context.Background(), "10.1038/nature12373")
	if err != nil {
		t.Fatal(err)
	}
	want := Work{
		DOI: "10.1038/nature12373", Title: "Nanometre-scale thermometry in a living cell & more",
		Authors: "Kucsko G., Maurer P. C., Harvard Collaboration, Sartre J.-P.", Venue: "Nature", Year: 2013, Type: TypeArticle,
		Volume: "500", Issue: "7460", Pages: "54-58", ISSNs: []string{"0028-0836", "1476-4687"},
	}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("получили %+v\nожидали %+v", w, want)
	}
	if f.paths[0] != "/works/10.1038/nature12373" {
		t.Errorf("путь запроса %q", f.paths[0])
	}
	if f.agents[0] != "SciBox/1.0 (mailto:ops@example.ru)" {
		t.Errorf("User-Agent %q", f.agents[0])
	}
}

func TestLookupWithoutMailto(t *testing.T) {
	f := &fake{status: 200, body: fullBody}
	c := newClient(t, f, "")
	if _, err := c.Lookup(context.Background(), "10.1038/nature12373"); err != nil {
		t.Fatal(err)
	}
	if f.agents[0] != "SciBox/1.0 (profile import)" {
		t.Errorf("User-Agent %q", f.agents[0])
	}
}

func TestLookupEscapesSpecialCharacters(t *testing.T) {
	f := &fake{status: 200, body: fullBody}
	c := newClient(t, f, "")
	if _, err := c.Lookup(context.Background(), "10.1002/a#b<c>"); err != nil {
		t.Fatal(err)
	}
	if f.paths[0] != "/works/10.1002/a%23b%3Cc%3E" {
		t.Errorf("путь запроса %q", f.paths[0])
	}
}

func TestLookupSparse(t *testing.T) {
	// Нет авторов, года-издания, названия; название строкой; дата из «created»; тип неизвестный.
	body := `{"message":{"DOI":"10.5555/Sparse","type":"standard","title":"Только строка","created":{"date-parts":[[2020,1,2]]},"issued":{"date-parts":[[null]]}}}`
	c := newClient(t, &fake{status: 200, body: body}, "")
	w, err := c.Lookup(context.Background(), "10.5555/sparse")
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "Только строка" || w.Year != 2020 || w.Type != TypeOther || w.Authors != "" || w.Venue != "" || w.DOI != "10.5555/sparse" || w.ISSNs == nil || len(w.ISSNs) != 0 {
		t.Errorf("получили %+v", w)
	}
}

func TestLookupWeirdValues(t *testing.T) {
	// title странного вида считается пустым; год берётся из печатной даты; автор только с именем / только с фамилией.
	body := `{"message":{"DOI":"10.5555/w","title":{"x":1},"container-title":["", "  Журнал  "],
	  "author":[{"given":"Анна"},{"family":"Иванова"},{"given":"- .","family":"Петров"}],
	  "published-print":{"date-parts":[[2018]]},"published-online":{"date-parts":[[2017]]},"ISSN":"2041-1723"}}`
	c := newClient(t, &fake{status: 200, body: body}, "")
	w, err := c.Lookup(context.Background(), "10.5555/w")
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "" || w.Venue != "Журнал" || w.Year != 2018 || w.Authors != "Анна, Иванова, Петров " || len(w.ISSNs) != 1 || w.ISSNs[0] != "2041-1723" {
		t.Errorf("получили %+v", w)
	}
}

func TestLookupErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"нет такого DOI", 404, "Resource not found.", ErrNotFound},
		{"слишком часто", 429, "", ErrUnavailable},
		{"сбой Crossref", 503, "", ErrUnavailable},
		{"не JSON", 200, "<html>", ErrUnavailable},
		{"JSON без работы", 200, `{"status":"ok"}`, ErrUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, &fake{status: tc.status, body: tc.body}, "")
			if _, err := c.Lookup(context.Background(), "10.1038/x"); !errors.Is(err, tc.want) {
				t.Errorf("ошибка %v, ожидали %v", err, tc.want)
			}
		})
	}
}

func TestLookupTooLarge(t *testing.T) {
	c := newClient(t, &fake{status: 200, body: strings.Repeat(" ", maxBody+10)}, "")
	if _, err := c.Lookup(context.Background(), "10.1038/x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ошибка %v", err)
	}
}

func TestLookupUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // адрес больше никто не слушает
	c := NewClient(url, "")
	if _, err := c.Lookup(context.Background(), "10.1038/x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ошибка %v", err)
	}
}

func TestLookupTimeoutAndCancel(t *testing.T) {
	f := &fake{status: 200, body: fullBody, delay: 300 * time.Millisecond}
	c := newClient(t, f, "")
	c.HTTP.Timeout = 50 * time.Millisecond
	if _, err := c.Lookup(context.Background(), "10.1038/x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("таймаут: ошибка %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Lookup(ctx, "10.1038/x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("отмена: ошибка %v", err)
	}
}

func TestLookupBadBaseURL(t *testing.T) {
	c := NewClient("http://bad host", "")
	if _, err := c.Lookup(context.Background(), "10.1038/x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ошибка %v", err)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.BaseURL != DefaultURL || c.HTTP.Timeout != timeout {
		t.Errorf("по умолчанию: %+v", c)
	}
}

func TestMapType(t *testing.T) {
	tests := map[string]string{
		"journal-article": TypeArticle, "book": TypeBook, "monograph": TypeBook, "edited-book": TypeBook, "reference-book": TypeBook,
		"book-chapter": TypeChapter, "book-section": TypeChapter, "reference-entry": TypeChapter,
		"proceedings-article": TypeConference, "proceedings": TypeConference, "posted-content": TypePreprint,
		"dissertation": TypeThesis, "dataset": TypeOther, "": TypeOther,
	}
	for in, want := range tests {
		if got := mapType(in); got != want {
			t.Errorf("mapType(%q) = %q, ожидали %q", in, got, want)
		}
	}
}

func TestJoinAuthorsTruncates(t *testing.T) {
	var names []string
	for i := 0; i < 200; i++ {
		names = append(names, "Фамилиев А. Б.")
	}
	got := joinAuthors(names)
	if !strings.HasSuffix(got, " и др.") || len(got) > maxAuthorsLen+len(" и др.") {
		t.Errorf("длина %d, конец %q", len(got), got[max(0, len(got)-20):])
	}
	if got := joinAuthors([]string{"А", "Б"}); got != "А, Б" {
		t.Errorf("короткий список: %q", got)
	}
	if got := joinAuthors(nil); got != "" {
		t.Errorf("пустой список: %q", got)
	}
}
