// Package crossref ищет публикацию по DOI в Crossref (https://api.crossref.org, без ключа) и приводит ответ
// к простым полям записи профиля. Разбор терпимый: у работы может не быть авторов, года, названия издания.
package crossref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Ошибки поиска.
var (
	// ErrNotFound — Crossref не знает такого DOI.
	ErrNotFound = errors.New("crossref: doi not found")
	// ErrUnavailable — Crossref не ответил или ответил непонятно: человек может заполнить поля вручную.
	ErrUnavailable = errors.New("crossref: service unavailable")
)

const (
	// DefaultURL — адрес API.
	DefaultURL = "https://api.crossref.org"
	// maxBody — самый большой разбираемый ответ: у работ со списком литературы он бывает в несколько мегабайт.
	maxBody = 8 << 20
	// maxAuthorsLen — сколько знаков отдаём в «авторы» (в записи профиля поле до 1000 знаков).
	maxAuthorsLen = 900
	timeout       = 8 * time.Second
)

// Типы публикаций, к которым сводятся типы Crossref.
const (
	TypeArticle    = "article"
	TypeBook       = "book"
	TypeChapter    = "chapter"
	TypeConference = "conference"
	TypePreprint   = "preprint"
	TypeThesis     = "thesis"
	TypeOther      = "other"
)

// Work — публикация, найденная по DOI.
type Work struct {
	DOI     string `json:"doi"`
	Title   string `json:"title"`
	Authors string `json:"authors"`
	Venue   string `json:"venue"`
	Year    int    `json:"year,omitempty"`
	Type    string `json:"type"`
	Volume  string `json:"volume"`
	Issue   string `json:"issue"`
	Pages   string `json:"pages"`
	// ISSNs — ISSN издания, как их отдал Crossref (печатный и электронный); проверяет и выбирает пакет profiles.
	ISSNs []string `json:"issns"`
}

// Client — клиент Crossref. Нулевое значение не годится: нужен BaseURL; NewClient подставляет настройки по умолчанию.
type Client struct {
	// BaseURL — адрес API без завершающей косой черты.
	BaseURL string
	// Mailto — почта для «вежливого пула» Crossref: попадает в User-Agent. Можно оставить пустой.
	Mailto string
	// HTTP — клиент с таймаутом.
	HTTP *http.Client
}

// NewClient собирает клиента; пустой baseURL означает настоящий Crossref.
func NewClient(baseURL, mailto string) *Client {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Mailto: mailto, HTTP: &http.Client{Timeout: timeout}}
}

var doiPattern = regexp.MustCompile(`^10\.[0-9]{4,9}/\S+$`)

// maxDOILen — длиннее настоящих DOI не бывает.
const maxDOILen = 200

var doiPrefixes = []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi.org/", "doi:"}

// NormalizeDOI приводит DOI к виду `10.1234/abc` в нижнем регистре: убирает адрес doi.org, приставку «doi:», пробелы
// и знаки препинания в конце (при копировании из текста туда часто попадает точка). ok == false, если это не DOI.
func NormalizeDOI(s string) (string, bool) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range doiPrefixes {
		if strings.HasPrefix(low, p) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	s = strings.ToLower(strings.TrimRight(s, ".,;"))
	if len(s) > maxDOILen || !doiPattern.MatchString(s) {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// escapeDOI кодирует DOI для пути: косая черта остаётся, «#», «?», «<» и пробелы кодируются.
func escapeDOI(doi string) string {
	parts := strings.Split(doi, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// Lookup находит публикацию. doi должен быть нормализован (NormalizeDOI).
func (c *Client) Lookup(ctx context.Context, doi string) (Work, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/works/"+escapeDOI(doi), nil)
	if err != nil {
		return Work{}, fmt.Errorf("%w: build request: %w", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	agent := "SciBox/1.0 (profile import)"
	if c.Mailto != "" {
		agent = "SciBox/1.0 (mailto:" + c.Mailto + ")"
	}
	req.Header.Set("User-Agent", agent)

	res, err := c.HTTP.Do(req)
	if err != nil {
		return Work{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }() // тело только читали: ошибка закрытия ничего не меняет
	switch {
	case res.StatusCode == http.StatusNotFound:
		return Work{}, ErrNotFound
	case res.StatusCode != http.StatusOK:
		return Work{}, fmt.Errorf("%w: status %d", ErrUnavailable, res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return Work{}, fmt.Errorf("%w: read body: %w", ErrUnavailable, err)
	}
	if len(raw) > maxBody {
		return Work{}, fmt.Errorf("%w: response is too large", ErrUnavailable)
	}
	return parse(raw)
}

// stringList принимает и строку, и список строк (Crossref отдаёт название списком, но терпим и к строке).
type stringList []string

func (l *stringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*l = stringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		*l = nil   // непонятное значение считаем пустым
		return nil //nolint:nilerr // намеренно: одно странное поле Crossref не должно ломать весь импорт публикации
	}
	*l = many
	return nil
}

func (l stringList) first() string {
	for _, s := range l {
		if s = clean(s); s != "" {
			return s
		}
	}
	return ""
}

type dateParts struct {
	Parts [][]*int `json:"date-parts"`
}

func (d dateParts) year() int {
	if len(d.Parts) > 0 && len(d.Parts[0]) > 0 && d.Parts[0][0] != nil {
		return *d.Parts[0][0]
	}
	return 0
}

type response struct {
	Message struct {
		DOI            string     `json:"DOI"`
		Type           string     `json:"type"`
		Title          stringList `json:"title"`
		ContainerTitle stringList `json:"container-title"`
		Volume         string     `json:"volume"`
		Issue          string     `json:"issue"`
		Page           string     `json:"page"`
		ISSN           stringList `json:"ISSN"`
		Author         []struct {
			Given  string `json:"given"`
			Family string `json:"family"`
			Name   string `json:"name"`
		} `json:"author"`
		Issued          dateParts `json:"issued"`
		PublishedPrint  dateParts `json:"published-print"`
		PublishedOnline dateParts `json:"published-online"`
		Created         dateParts `json:"created"`
	} `json:"message"`
}

func parse(raw []byte) (Work, error) {
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return Work{}, fmt.Errorf("%w: parse response: %w", ErrUnavailable, err)
	}
	m := r.Message
	if m.DOI == "" {
		return Work{}, fmt.Errorf("%w: response without message", ErrUnavailable)
	}
	w := Work{
		DOI:    strings.ToLower(m.DOI),
		Title:  m.Title.first(),
		Venue:  m.ContainerTitle.first(),
		Type:   mapType(m.Type),
		Volume: clean(m.Volume),
		Issue:  clean(m.Issue),
		Pages:  clean(m.Page),
		ISSNs:  []string{},
	}
	for _, issn := range m.ISSN {
		if issn = clean(issn); issn != "" {
			w.ISSNs = append(w.ISSNs, issn)
		}
	}
	for _, d := range []dateParts{m.Issued, m.PublishedPrint, m.PublishedOnline, m.Created} {
		if y := d.year(); y > 0 {
			w.Year = y
			break
		}
	}
	names := make([]string, 0, len(m.Author))
	for _, a := range m.Author {
		if n := authorName(a.Given, a.Family, a.Name); n != "" {
			names = append(names, n)
		}
	}
	w.Authors = joinAuthors(names)
	return w, nil
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// clean убирает из текста Crossref разметку (<i>, <sub>, JATS), HTML-сущности и лишние пробелы.
func clean(s string) string {
	s = tagPattern.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// authorName: «Иванов И. И.»; у организации-автора берётся её название.
func authorName(given, family, org string) string {
	given, family, org = clean(given), clean(family), clean(org)
	switch {
	case family == "" && given == "":
		return org
	case family == "":
		return given
	case given == "":
		return family
	}
	return family + " " + initials(given)
}

// initials: «Ivan Ivanovich» → «I. I.», «Jean-Paul» → «J.-P.».
func initials(given string) string {
	var out []string
	for _, word := range strings.Fields(given) {
		var parts []string
		for _, piece := range strings.Split(word, "-") {
			for _, r := range piece {
				if unicode.IsLetter(r) {
					parts = append(parts, string(unicode.ToUpper(r))+".")
					break
				}
			}
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, "-"))
		}
	}
	return strings.Join(out, " ")
}

// joinAuthors соединяет имена через запятую; слишком длинный список обрывается на границе автора словами «и др.».
func joinAuthors(names []string) string {
	var b strings.Builder
	for i, n := range names {
		next := n
		if i > 0 {
			next = ", " + n
		}
		if b.Len()+len(next) > maxAuthorsLen {
			b.WriteString(" и др.")
			return b.String()
		}
		b.WriteString(next)
	}
	return b.String()
}

// mapType сводит тип Crossref к простому списку типов записи профиля.
func mapType(t string) string {
	switch t {
	case "journal-article":
		return TypeArticle
	case "book", "monograph", "edited-book", "reference-book":
		return TypeBook
	case "book-chapter", "book-section", "reference-entry":
		return TypeChapter
	case "proceedings-article", "proceedings":
		return TypeConference
	case "posted-content":
		return TypePreprint
	case "dissertation":
		return TypeThesis
	}
	return TypeOther
}
