// Package cv рисует PDF с резюме по готовому описанию документа. Пакет ничего не знает про профили и приватность:
// что попадёт в документ, решает вызывающий (internal/profiles). Шрифты Golos Text и Literata лежат в проекте
// (лицензия SIL OFL, fonts/OFL.txt): в них есть кириллица, а запросов к чужим серверам нет.
package cv

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
)

//go:embed fonts/GolosText-Regular.ttf
var golosRegular []byte

//go:embed fonts/GolosText-Bold.ttf
var golosBold []byte

//go:embed fonts/Literata-Regular.ttf
var literataRegular []byte

//go:embed fonts/Literata-SemiBold.ttf
var literataSemiBold []byte

// Document — что нарисовать.
type Document struct {
	Title    string    // имя человека
	Subtitle string    // должность, место работы
	Meta     []string  // строки под заголовком: город, контакты, идентификаторы
	Sections []Section // разделы по порядку
	Footer   string    // подпись внизу каждой страницы, например «SciBox · 3 октября 2026»
	Created  time.Time // дата в свойствах файла
}

// Section — раздел: заголовок, затем абзац и/или записи.
type Section struct {
	Heading   string
	Paragraph string
	Entries   []Entry
}

// Entry — запись раздела: слева метка (годы, номер), справа жирная строка и обычный текст.
type Entry struct {
	Label string
	Lead  string
	Text  string
}

// Размеры в миллиметрах и пунктах.
const (
	marginX     = 20.0
	marginTop   = 18.0
	marginBot   = 18.0
	labelWidth  = 27.0
	lineHeight  = 4.7
	entryGap    = 2.2
	sectionGap  = 5.0
	pageHeight  = 297.0
	contentW    = 210.0 - 2*marginX
	entryTextW  = contentW - labelWidth
	titleSize   = 22.0
	bodySize    = 9.6
	headingSize = 12.5
)

// Цвета из дизайн-системы «Журнал»: тёплый чёрный, чернильно-синий, серый для второстепенного.
var (
	ink  = [3]int{0x1F, 0x1D, 0x1A}
	blue = [3]int{0x1C, 0x2C, 0x66}
	mute = [3]int{0x6B, 0x66, 0x5E}
)

func color(p *fpdf.Fpdf, c [3]int) { p.SetTextColor(c[0], c[1], c[2]) }

// safe убирает из текста то, что библиотека PDF не умеет рисовать: символы вне основной плоскости Юникода (эмодзи)
// и управляющие знаки, кроме перевода строки. Символы, которых нет в шрифте, рисуются пустым местом.
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < ' ' || (r >= 0x7f && r < 0xa0) || r > 0xFFFF || (r >= 0xD800 && r <= 0xDFFF):
			return -1
		}
		return r
	}, s)
}

func (d Document) clean() Document {
	d.Title, d.Subtitle, d.Footer = safe(d.Title), safe(d.Subtitle), safe(d.Footer)
	d.Meta = mapAll(d.Meta, safe)
	secs := make([]Section, len(d.Sections))
	for i, s := range d.Sections {
		entries := make([]Entry, len(s.Entries))
		for j, e := range s.Entries {
			entries[j] = Entry{Label: safe(e.Label), Lead: safe(e.Lead), Text: safe(e.Text)}
		}
		secs[i] = Section{Heading: safe(s.Heading), Paragraph: safe(s.Paragraph), Entries: entries}
	}
	d.Sections = secs
	return d
}

func mapAll(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// Render собирает PDF.
func Render(doc Document) ([]byte, error) {
	doc = doc.clean()
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(marginX, marginTop, marginX)
	p.SetAutoPageBreak(true, marginBot)
	p.SetCompression(true)
	p.AddUTF8FontFromBytes("Golos", "", golosRegular)
	p.AddUTF8FontFromBytes("Golos", "B", golosBold)
	p.AddUTF8FontFromBytes("Literata", "", literataRegular)
	p.AddUTF8FontFromBytes("Literata", "B", literataSemiBold)
	p.SetTitle(doc.Title, true)
	p.SetCreator("SciBox", true)
	if !doc.Created.IsZero() {
		p.SetCreationDate(doc.Created)
	}
	p.AliasNbPages("{nb}")
	p.SetFooterFunc(func() {
		p.SetY(-12)
		p.SetFont("Golos", "", 8)
		color(p, mute)
		p.CellFormat(contentW/2, 4, doc.Footer, "", 0, "L", false, 0, "")
		p.CellFormat(contentW/2, 4, fmt.Sprintf("%d / {nb}", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.AddPage()

	p.SetFont("Literata", "B", titleSize)
	color(p, ink)
	p.MultiCell(contentW, 9, doc.Title, "", "L", false)
	if doc.Subtitle != "" {
		p.SetFont("Golos", "", 11)
		color(p, mute)
		p.MultiCell(contentW, 5.4, doc.Subtitle, "", "L", false)
	}
	if len(doc.Meta) > 0 {
		p.Ln(1.5)
		p.SetFont("Golos", "", bodySize)
		color(p, ink)
		for _, m := range doc.Meta {
			p.MultiCell(contentW, lineHeight, m, "", "L", false)
		}
	}
	for _, s := range doc.Sections {
		section(p, s)
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("cv: draw: %w", err)
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, fmt.Errorf("cv: output: %w", err)
	}
	return buf.Bytes(), nil
}

// ensure переносит рисование на новую страницу, если height не помещается.
func ensure(p *fpdf.Fpdf, height float64) {
	if p.GetY()+height > pageHeight-marginBot {
		p.AddPage()
	}
}

func section(p *fpdf.Fpdf, s Section) {
	// Заголовок не остаётся один внизу страницы: ему нужно место ещё на первую запись (но не больше полстраницы:
	// длинную запись всё равно придётся переносить).
	need := 16.0
	switch {
	case len(s.Entries) > 0:
		need += entryHeight(p, s.Entries[0])
	case s.Paragraph != "":
		need += 3 * lineHeight
	}
	ensure(p, min(need, pageHeight/2))
	p.Ln(sectionGap)
	p.SetFont("Literata", "B", headingSize)
	color(p, blue)
	p.MultiCell(contentW, 6, s.Heading, "", "L", false)
	y := p.GetY() + 0.6
	p.SetDrawColor(blue[0], blue[1], blue[2])
	p.SetLineWidth(0.3)
	p.Line(marginX, y, marginX+contentW, y)
	p.SetY(y + 2.2)

	if s.Paragraph != "" {
		p.SetFont("Golos", "", bodySize)
		color(p, ink)
		p.MultiCell(contentW, lineHeight, strings.TrimSpace(s.Paragraph), "", "L", false)
	}
	for _, e := range s.Entries {
		entry(p, e)
	}
}

// entryHeight — сколько места займёт запись; шрифт после вызова не меняется для вызывающего (его выставляет рисование).
func entryHeight(p *fpdf.Fpdf, e Entry) float64 {
	p.SetFont("Golos", "B", bodySize)
	leadLines := len(p.SplitText(e.Lead, entryTextW))
	if e.Lead == "" {
		leadLines = 0
	}
	p.SetFont("Golos", "", bodySize)
	textLines := len(p.SplitText(e.Text, entryTextW))
	if e.Text == "" {
		textLines = 0
	}
	labelLines := len(p.SplitText(e.Label, labelWidth))
	if e.Label == "" {
		labelLines = 0
	}
	return float64(max(leadLines+textLines, labelLines, 1)) * lineHeight
}

func entry(p *fpdf.Fpdf, e Entry) {
	ensure(p, entryHeight(p, e))

	top := p.GetY()
	p.SetXY(marginX, top)
	p.SetFont("Golos", "", 9)
	color(p, mute)
	if e.Label != "" {
		p.MultiCell(labelWidth-2, lineHeight, e.Label, "", "L", false)
	}
	bottom := p.GetY()

	p.SetXY(marginX+labelWidth, top)
	color(p, ink)
	if e.Lead != "" {
		p.SetFont("Golos", "B", bodySize)
		p.MultiCell(entryTextW, lineHeight, e.Lead, "", "L", false)
		p.SetX(marginX + labelWidth)
	}
	if e.Text != "" {
		p.SetFont("Golos", "", bodySize)
		p.MultiCell(entryTextW, lineHeight, e.Text, "", "L", false)
	}
	p.SetXY(marginX, max(bottom, p.GetY())+entryGap)
}
