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

// Document — что нарисовать. Первая страница открывается синей плашкой с именем, под ней полоса с числами (до четырёх),
// затем строка идентификаторов и пояснения; дальше разделы.
type Document struct {
	Title    string    // имя человека
	Subtitle string    // должность, место работы
	Where    []string  // в плашке под должностью: город, почта
	Stats    []Fact    // крупные числа под плашкой, до четырёх; в подписи «\n» делит строку, например «h-index\nРИНЦ»
	IDs      []Fact    // идентификаторы одной строкой: «ORCID 0000-…»
	Notes    []string  // мелкие пояснения под идентификаторами
	Sections []Section // разделы по порядку
	Footer   string    // подпись внизу каждой страницы, например «SciBox · 3 октября 2026»
	Created  time.Time // дата в свойствах файла
}

// Fact — пара «название, значение»: число с подписью или идентификатор.
type Fact struct {
	Label string
	Value string
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
	bodySize    = 9.6
	headingSize = 12.5

	pageWidth    = 210.0
	bandTop      = 15.0 // отступ от верха страницы до имени
	bandBottom   = 10.0
	titleSize    = 26.0
	titleLine    = 10.6
	subtitleSize = 11.5
	subtitleLine = 5.6
	subtitleW    = 160.0
	whereSize    = 9.2
	whereLine    = 4.4
	markWidth    = 26.0 // жёлтая черта на нижнем краю плашки
	markHeight   = 1.6

	statCells   = 4
	statValue   = 22.0
	statLabel   = 7.0
	statPadTop  = 6.0
	statPadBot  = 5.5
	statValueH  = 9.0
	statLabelH  = 3.0
	statLabels  = 2
	idsGapTop   = 3.8
	idsSize     = 8.6
	idsLine     = 4.4
	noteSize    = 8.6
	whereGapTop = 5.0
)

// Цвета из дизайн-системы «Журнал» (DESIGN.md): тёплый чёрный, чернильно-синий, серый для второстепенного, один жёлтый маркер.
var (
	ink      = [3]int{0x1F, 0x1D, 0x1A}
	blue     = [3]int{0x1C, 0x2C, 0x66}
	mute     = [3]int{0x5D, 0x5A, 0x55}
	white    = [3]int{0xFF, 0xFF, 0xFF}
	onBlue   = [3]int{0xD3, 0xD6, 0xEA}
	wash     = [3]int{0xF6, 0xF4, 0xF1}
	hairline = [3]int{0xD9, 0xD6, 0xD1}
	marker   = [3]int{0xF7, 0xD5, 0x59}
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
	d.Where, d.Notes = mapAll(d.Where, safe), mapAll(d.Notes, safe)
	d.Stats, d.IDs = cleanFacts(d.Stats), cleanFacts(d.IDs)
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

func cleanFacts(in []Fact) []Fact {
	out := make([]Fact, len(in))
	for i, f := range in {
		out[i] = Fact{Label: safe(f.Label), Value: safe(f.Value)}
	}
	return out
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
	header(p, doc)
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

func fill(p *fpdf.Fpdf, c [3]int) { p.SetFillColor(c[0], c[1], c[2]) }

// header рисует первую страницу сверху вниз: плашка с именем, полоса с числами, идентификаторы, пояснения.
// Плашка занимает всю ширину листа, её высота зависит от того, сколько строк заняли имя и должность.
func header(p *fpdf.Fpdf, doc Document) {
	p.SetFont("Literata", "B", titleSize)
	titleLines := len(p.SplitText(doc.Title, contentW))
	p.SetFont("Golos", "", subtitleSize)
	subLines := 0
	if doc.Subtitle != "" {
		subLines = len(p.SplitText(doc.Subtitle, subtitleW))
	}
	h := bandTop + float64(titleLines)*titleLine + bandBottom
	if subLines > 0 {
		h += 2.4 + float64(subLines)*subtitleLine
	}
	if len(doc.Where) > 0 {
		h += whereGapTop + whereLine
	}
	fill(p, blue)
	p.Rect(0, 0, pageWidth, h, "F")
	fill(p, marker)
	p.Rect(marginX, h-markHeight, markWidth, markHeight, "F")

	p.SetXY(marginX, bandTop)
	p.SetFont("Literata", "B", titleSize)
	color(p, white)
	p.MultiCell(contentW, titleLine, doc.Title, "", "L", false)
	if subLines > 0 {
		p.SetXY(marginX, p.GetY()+2.4)
		p.SetFont("Golos", "", subtitleSize)
		color(p, onBlue)
		p.MultiCell(subtitleW, subtitleLine, doc.Subtitle, "", "L", false)
	}
	if len(doc.Where) > 0 {
		p.SetXY(marginX, p.GetY()+whereGapTop)
		p.SetFont("Golos", "", whereSize)
		for i, w := range doc.Where {
			if i == 0 {
				color(p, white)
			} else {
				color(p, onBlue)
			}
			p.CellFormat(p.GetStringWidth(w)+7, whereLine, w, "", 0, "L", false, 0, "")
		}
	}

	y := h
	if len(doc.Stats) > 0 {
		y = stats(p, doc.Stats, y)
	}
	p.SetXY(marginX, y+idsGapTop)
	if len(doc.IDs) > 0 {
		ids(p, doc.IDs)
	}
	p.SetFont("Golos", "", noteSize)
	color(p, mute)
	for _, n := range doc.Notes {
		p.SetX(marginX)
		p.MultiCell(contentW, idsLine, n, "", "L", false)
	}
}

// stats рисует полосу с числами на светлом фоне от y и возвращает её нижний край. Столбцы равной ширины по четыре,
// поэтому первое число всегда стоит у левого поля.
func stats(p *fpdf.Fpdf, facts []Fact, y float64) float64 {
	h := statPadTop + statValueH + 1.6 + statLabels*statLabelH + statPadBot
	fill(p, wash)
	p.Rect(0, y, pageWidth, h, "F")
	p.SetDrawColor(hairline[0], hairline[1], hairline[2])
	p.SetLineWidth(0.3)
	p.Line(0, y+h, pageWidth, y+h)
	cell := contentW / statCells
	for i, f := range facts[:min(len(facts), statCells)] {
		x := marginX + float64(i)*cell
		p.SetXY(x, y+statPadTop)
		p.SetFont("Literata", "B", statValue)
		color(p, blue)
		p.CellFormat(cell, statValueH, f.Value, "", 0, "L", false, 0, "")
		p.SetFont("Golos", "B", statLabel)
		color(p, mute)
		for j, line := range strings.SplitN(strings.ToUpper(f.Label), "\n", statLabels) {
			p.SetXY(x, y+statPadTop+statValueH+1.6+float64(j)*statLabelH)
			p.CellFormat(cell, statLabelH, line, "", 0, "L", false, 0, "")
		}
	}
	return y + h
}

// ids пишет идентификаторы одной строкой с переносом: название жирным, значение обычным.
func ids(p *fpdf.Fpdf, facts []Fact) {
	for _, f := range facts {
		p.SetFont("Golos", "B", idsSize)
		color(p, ink)
		p.Write(idsLine, f.Label+" ")
		p.SetFont("Golos", "", idsSize)
		color(p, mute)
		p.Write(idsLine, f.Value+"     ")
	}
	p.Ln(idsLine)
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
