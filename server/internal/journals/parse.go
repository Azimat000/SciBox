// Package journals — справочник научных журналов с квартилями (срез 14, D-126, D-127): разбор файла SCImago Journal Rank,
// загрузка в базу заменой целиком, поиск журнала по названию и ISSN для формы публикации.
//
// Квартиль журнала — «лучший квартиль» SCImago (SJR Best Quartile): лучший из квартилей журнала по его предметным
// категориям за год данных файла. Публикация в профиле связана с журналом своим ISSN.
package journals

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"scibox/server/internal/issn"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Journal — журнал справочника.
type Journal struct {
	ID        int64
	Title     string
	Publisher string
	Quartile  int // 1–4; 0 — квартиля нет
	SJR       *float32
	ISSNs     []string // в порядке файла, без повторов и без ISSN, уже занятых журналом выше по списку
}

// Parsed — разобранный файл.
type Parsed struct {
	Year     int
	Journals []Journal
	// Skipped — строки без единого годного ISSN (такой журнал не связать с публикацией).
	Skipped int
}

// Пределы, за которыми файл считается не тем.
const (
	maxTitle     = 500
	maxPublisher = 300
)

var yearColumn = regexp.MustCompile(`^Total Docs\. \((\d{4})\)$`)

// ErrFormat — файл не похож на выгрузку SCImago.
var ErrFormat = errors.New("journals: not a SCImago journal rank file")

// columns — номера нужных столбцов по заголовку файла.
type columns struct {
	id, title, issn, publisher, sjr, quartile int
	year                                      int
}

func header(row []string) (columns, error) {
	c := columns{id: -1, title: -1, issn: -1, publisher: -1, sjr: -1, quartile: -1}
	for i, name := range row {
		name = strings.TrimSpace(strings.TrimPrefix(name, "\uFEFF"))
		switch name {
		case "Sourceid":
			c.id = i
		case "Title":
			c.title = i
		case "Issn":
			c.issn = i
		case "Publisher":
			if c.publisher < 0 { // в файле два столбца Publisher: берём первый
				c.publisher = i
			}
		case "SJR":
			c.sjr = i
		case "SJR Best Quartile":
			c.quartile = i
		default:
			if m := yearColumn.FindStringSubmatch(name); m != nil {
				c.year, _ = strconv.Atoi(m[1])
			}
		}
	}
	if c.id < 0 || c.title < 0 || c.issn < 0 || c.quartile < 0 || c.year == 0 {
		return c, fmt.Errorf("%w: missing columns", ErrFormat)
	}
	return c, nil
}

// Parse читает выгрузку SCImago (journalrank.php?out=xls): разделитель «;», ISSN без дефиса через запятую, десятичная
// запятая в SJR, «-» вместо квартиля. Год данных берётся из заголовка «Total Docs. (2025)». Строки идут по рангу, поэтому
// ISSN, который встретился у двух журналов, остаётся за тем, что выше.
func Parse(r io.Reader) (Parsed, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true // в файле встречаются кавычки внутри названий без удвоения: «"Center "Name" City"»
	cr.ReuseRecord = true
	head, err := cr.Read()
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: read header: %v", ErrFormat, err)
	}
	c, err := header(head)
	if err != nil {
		return Parsed{}, err
	}
	out := Parsed{Year: c.year}
	seenISSN := map[string]bool{}
	seenID := map[int64]bool{}
	for line := 2; ; line++ {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Parsed{}, fmt.Errorf("%w: line %d: %v", ErrFormat, line, err)
		}
		j, err := journalOf(row, c)
		if err != nil {
			return Parsed{}, fmt.Errorf("%w: line %d: %v", ErrFormat, line, err)
		}
		if seenID[j.ID] {
			return Parsed{}, fmt.Errorf("%w: line %d: repeated Sourceid %d", ErrFormat, line, j.ID)
		}
		seenID[j.ID] = true
		var issns []string
		for _, raw := range strings.Split(cellOf(row, c.issn), ",") {
			if n, ok := issn.Normalize(raw); ok && !seenISSN[n] {
				seenISSN[n] = true
				issns = append(issns, n)
			}
		}
		if len(issns) == 0 {
			out.Skipped++
			continue
		}
		j.ISSNs = issns
		out.Journals = append(out.Journals, j)
	}
	if len(out.Journals) == 0 {
		return Parsed{}, fmt.Errorf("%w: no journals", ErrFormat)
	}
	return out, nil
}

// cellOf — значение столбца без пробелов по краям; короткая строка даёт пустое значение.
func cellOf(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func journalOf(row []string, c columns) (Journal, error) {
	cell := func(i int) string { return cellOf(row, i) }
	id, err := strconv.ParseInt(cell(c.id), 10, 64)
	if err != nil || id <= 0 {
		return Journal{}, fmt.Errorf("bad Sourceid %q", cell(c.id))
	}
	j := Journal{ID: id, Title: strings.Join(strings.Fields(cell(c.title)), " "), Publisher: strings.Join(strings.Fields(cell(c.publisher)), " ")}
	if j.Title == "" || !utf8.ValidString(j.Title) || utf8.RuneCountInString(j.Title) > maxTitle {
		return Journal{}, fmt.Errorf("bad title for %d", id)
	}
	if !utf8.ValidString(j.Publisher) || utf8.RuneCountInString(j.Publisher) > maxPublisher {
		j.Publisher = ""
	}
	switch q := cell(c.quartile); q {
	case "Q1", "Q2", "Q3", "Q4":
		j.Quartile = int(q[1] - '0')
	case "", "-":
	default:
		return Journal{}, fmt.Errorf("bad quartile %q for %d", q, id)
	}
	if raw := strings.ReplaceAll(cell(c.sjr), ",", "."); raw != "" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && v >= 0 {
			f := float32(v)
			j.SJR = &f
		}
	}
	return j, nil
}
