package cv

import (
	"bytes"
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
		Meta:    []string{"Новосибирск, Новосибирская область", "orlova@example.ru", "ORCID 0000-0002-1825-0097 · SPIN 1234-5678"},
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
		Meta:     []string{strings.Repeat("a", 400), "https://example.ru/" + strings.Repeat("x", 300)},
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
