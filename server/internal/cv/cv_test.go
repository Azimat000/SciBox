package cv

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"
	"testing"
	"time"
)

func pages(pdf []byte) int { return bytes.Count(pdf, []byte("/Type /Page\n")) }

func isPDF(t *testing.T, b []byte) {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(b), []byte("%%EOF")) {
		t.Fatalf("это не PDF: начало %q", b[:min(20, len(b))])
	}
}

func fullDoc() Document {
	return Document{
		Title: "Елена Орлова", Subtitle: "Старший научный сотрудник, Сибирский институт квантовых материалов",
		Where:   []string{"Новосибирск, Новосибирская область", "orlova@example.ru"},
		IDs:     []Fact{{"ORCID", "0000-0002-1825-0097"}, {"SPIN", "1234-5678"}, {"h-index", "РИНЦ 12, Scopus 9"}},
		Notes:   []string{"Статей в журналах Q1–Q2: 7"},
		Footer:  "SciBox · 3 октября 2026",
		Created: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
		Sections: []Section{
			{Heading: "О себе", Paragraph: "Изучаю сверхпроводящие плёнки.\nЛюблю низкие температуры."},
			{Heading: "Публикации", Entries: []Entry{
				{Label: "1.", Lead: "Критическая температура плёнок", Text: "Орлова Е. А., Белов И. С. // Журнал физики. 2023."},
				{Label: "2020 — н. в.", Text: "Только текст без выделенной строки"},
				{Lead: "Только выделенная строка"},
				{},
			}},
		},
	}
}

func TestRenderFull(t *testing.T) {
	b, err := Render(fullDoc())
	if err != nil {
		t.Fatal(err)
	}
	isPDF(t, b)
	if n := pages(b); n != 1 {
		t.Errorf("страниц %d, ожидали 1", n)
	}
	other := fullDoc()
	other.Title = "Другое имя"
	diff, err := Render(other)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(b, diff) {
		t.Error("разные документы дали одинаковый файл")
	}
}

func TestRenderEmpty(t *testing.T) {
	b, err := Render(Document{})
	if err != nil {
		t.Fatal(err)
	}
	isPDF(t, b)
}

func TestRenderManyPages(t *testing.T) {
	doc := fullDoc()
	for i := 0; i < 5; i++ {
		var entries []Entry
		for j := 0; j < 40; j++ {
			entries = append(entries, Entry{Label: "2023", Lead: "Название работы номер", Text: strings.Repeat("Длинное описание записи с несколькими предложениями. ", 4)})
		}
		doc.Sections = append(doc.Sections, Section{Heading: "Раздел", Entries: entries})
	}
	b, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	isPDF(t, b)
	if n := pages(b); n < 5 {
		t.Errorf("страниц %d, ожидали не меньше 5", n)
	}
}

func TestSafe(t *testing.T) {
	tests := map[string]string{
		"Иванов И. И.": "Иванов И. И.",
		"a\tb":         "a b",
		"a\x00b\x07c":  "abc",
		"два\nстроки":  "два\nстроки",
		"смайл 🙂 тут":  "смайл  тут",
		"Šimon α≤β":    "Šimon α≤β",
		"c1\u0085ctl":  "c1ctl",
		"":             "",
	}
	for in, want := range tests {
		if got := safe(in); got != want {
			t.Errorf("safe(%q) = %q, ожидали %q", in, got, want)
		}
	}
}

func TestRenderOddText(t *testing.T) {
	doc := Document{
		Title:    strings.Repeat("Длиннофамильный", 20), // одно слово во много строк
		Subtitle: "Эмодзи 🙂 и иероглифы 漢字, которых нет в шрифте",
		Where:    []string{strings.Repeat("a", 400), "https://example.ru/" + strings.Repeat("x", 300)},
		IDs:      []Fact{{strings.Repeat("Метка", 40), strings.Repeat("0", 400)}},
		Notes:    []string{strings.Repeat("Пояснение ", 80), "🙂"},
		Sections: []Section{{Heading: strings.Repeat("Заголовок ", 30), Entries: []Entry{
			{Label: strings.Repeat("МетокМного", 8), Lead: strings.Repeat("Я", 600), Text: "<b>&amp;</b> \\ ( ) %"},
		}}},
	}
	b, err := Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	isPDF(t, b)
}

// content распаковывает потоки страниц PDF: в них записаны цвета заливки и текста.
func content(t *testing.T, pdf []byte) string {
	t.Helper()
	var out strings.Builder
	rest := pdf
	for {
		_, after, ok := bytes.Cut(rest, []byte("stream\n"))
		if !ok {
			break
		}
		body, next, ok := bytes.Cut(after, []byte("\nendstream"))
		if !ok {
			break
		}
		rest = next
		r, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			continue // шрифты и прочие двоичные потоки
		}
		raw, err := io.ReadAll(r)
		if err == nil {
			out.Write(raw)
		}
	}
	return out.String()
}

// TestRenderHasNoAccentBars: в резюме нет жёлтой черты на плашке и серой полосы с числами под ней (D-208).
func TestRenderHasNoAccentBars(t *testing.T) {
	const (
		blueFill   = "0.110 0.173 0.400 rg" // плашка с именем: проверка, что потоки читаются
		yellowFill = "0.969 0.835 0.349 rg"
		washFill   = "0.965 0.957 0.945 rg"
	)
	docs := map[string]Document{
		"полный":  fullDoc(),
		"пустой":  {Title: "Только имя"},
		"без ids": {Title: "Имя", Subtitle: "Должность", Where: []string{"Томск"}},
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			pdf, err := Render(doc)
			if err != nil {
				t.Fatal(err)
			}
			c := content(t, pdf)
			if !strings.Contains(c, blueFill) {
				t.Fatalf("синей плашки не нашли, поток не прочитан: %.200q", c)
			}
			for _, bad := range []string{yellowFill, washFill} {
				if strings.Contains(c, bad) {
					t.Errorf("в файле остался лишний цвет %s", bad)
				}
			}
		})
	}
}

// TestRenderBandHeightFollowsText: плашка по высоте зависит только от имени, должности и строки «где»; идентификаторы
// под ней ничего не сдвигают и не оставляют пустого места, если их нет.
func TestRenderBandHeightFollowsText(t *testing.T) {
	bare, err := Render(Document{Title: "Имя", Subtitle: "Должность"})
	if err != nil {
		t.Fatal(err)
	}
	withIDs, err := Render(Document{Title: "Имя", Subtitle: "Должность", IDs: []Fact{{"h-index", "РИНЦ 9"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Плашка рисуется первой: прямоугольник «0.00 <верх> 595.28 -<высота> re f» (ширина листа в пунктах) одинаков в обоих файлах.
	rect := func(c string) string {
		for line := range strings.SplitSeq(c, "\n") {
			if strings.HasSuffix(line, " re f") && strings.Contains(line, "595.28") {
				return line
			}
		}
		return ""
	}
	a, b := rect(content(t, bare)), rect(content(t, withIDs))
	if a == "" || a != b {
		t.Errorf("плашка отличается: %q и %q", a, b)
	}
}
