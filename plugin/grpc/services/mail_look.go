package services

import (
	"html"
	"image/color"
	"strings"
	"sync"

	"github.com/teranos/QNTX/web"
	"github.com/teranos/errors"
)

// "it just needs to fit with the rest of qntx"
//
// A mail QNTX draws is an element window, the way ≡ am is one: a title bar,
// then sections of labelled rows. Every value is read from web/css when the
// mail is drawn. Only tables and inline styles carry it, because those are what
// every mail client lays out the same way.

// windowColour is what @teranos/elements paints a window when nothing asks for
// another: DEFAULT_COLOR in element.ts, at the version web/package.json pins.
// The package is not in this repository, so its value is written here.
const windowColour = "rgba(35, 35, 38, 0.92)"

// look is the values a mail is drawn in, each read from the rule named beside it.
type look struct {
	canvas, grid string // .canvas-workspace
	window       string // windowColour over the canvas, as one colour
	mono         string // --font-mono

	barHeight, barPadding, barBorder, barGap string // .title-bar
	titleColor, titleSize, titleWeight       string // .title-bar > .symbol + span

	windowPadding                     string // .element-window-content
	contentPadding, size, lineHeight string // .element-content
	sectionGap                        string // .element-section

	headingMargin, headingPadding, headingSize, headingWeight, headingColor string // .element-section-title

	rowPadding, rowBorder, lastRowBorder string // .element-row, .element-row:last-child
	labelColor, labelSize                string // .label
	valueColor                           string // .element-value
	well, unwell, note                   string // .status-well, .status-unwell, .element-note
	keyBreak                             string // .element-did

	graphRule, graphFill, graphLine string // --border-on-dark, --element-status-success-bg, --accent-on-dark
}

var qntxLook = sync.OnceValues(readLook)

func readLook() (look, error) {
	s, err := readSheet(web.CSS, "css/tokens.css", "css/canvas.css", "css/window.css",
		"css/element/title-bar.css", "css/element/states/window.css")
	if err != nil {
		return look{}, err
	}
	var l look
	var failed error
	prop := func(into *string, selector, property string) {
		if failed == nil {
			*into, failed = s.prop(selector, property)
		}
	}
	token := func(into *string, name string) {
		if failed == nil {
			*into, failed = s.token(name)
		}
	}

	prop(&l.canvas, ".canvas-workspace", "background-color")
	prop(&l.grid, ".canvas-workspace", "background-image")
	token(&l.mono, "--font-mono")

	prop(&l.barHeight, ".title-bar", "height")
	prop(&l.barPadding, ".title-bar", "padding")
	prop(&l.barBorder, ".title-bar", "border-bottom")
	prop(&l.barGap, ".title-bar", "gap")
	prop(&l.titleColor, ".title-bar > .symbol + span", "color")
	prop(&l.titleSize, ".title-bar > .symbol + span", "font-size")
	prop(&l.titleWeight, ".title-bar > .symbol + span", "font-weight")

	prop(&l.windowPadding, ".element-window-content", "padding")
	prop(&l.contentPadding, ".element-content", "padding")
	prop(&l.size, ".element-content", "font-size")
	prop(&l.lineHeight, ".element-content", "line-height")
	prop(&l.sectionGap, ".element-section", "margin-bottom")

	prop(&l.headingMargin, ".element-section-title", "margin")
	prop(&l.headingPadding, ".element-section-title", "padding")
	prop(&l.headingSize, ".element-section-title", "font-size")
	prop(&l.headingWeight, ".element-section-title", "font-weight")
	prop(&l.headingColor, ".element-section-title", "color")

	prop(&l.rowPadding, ".element-row", "padding")
	prop(&l.rowBorder, ".element-row", "border-bottom")
	prop(&l.lastRowBorder, ".element-row:last-child", "border-bottom")
	prop(&l.labelColor, ".label", "color")
	prop(&l.labelSize, ".label", "font-size")
	prop(&l.valueColor, ".element-value", "color")
	prop(&l.well, ".status-well", "color")
	prop(&l.unwell, ".status-unwell", "color")
	prop(&l.note, ".element-note", "color")
	prop(&l.keyBreak, ".element-did", "word-break")

	token(&l.graphRule, "--border-on-dark")
	token(&l.graphFill, "--element-status-success-bg")
	token(&l.graphLine, "--accent-on-dark")
	if failed != nil {
		return look{}, errors.Wrap(failed, "a mail cannot be drawn from web/css")
	}

	// A mail client that reads no rgba would drop the window; the colour the
	// eye sees on the canvas is written instead.
	over, err := ColorOf(l.canvas)
	if err != nil {
		return look{}, errors.Wrap(err, "the canvas colour")
	}
	window, err := ColorOf(windowColour)
	if err != nil {
		return look{}, errors.Wrap(err, "the window colour")
	}
	l.window = hexOf(composite(window, over))
	return l, nil
}

// MailWindow is one window of a mail QNTX draws.
type MailWindow struct {
	Symbol   string // one of web/ts/sym.ts; empty draws the title alone
	Title    string
	Sections []MailSection
}

// MailSection is an element section: a heading, then what it holds.
type MailSection struct {
	Title string    // empty draws no heading
	HTML  string    // placed as it is, before the rows: an image, a paragraph
	Rows  []MailRow // drawn as .element-row
}

// MailRow is an element row: a label on the left, its value on the right.
type MailRow struct {
	Label, Value string
	State        RowState
	Key          bool // a key read character by character, as .element-did: it breaks anywhere
}

// RowState is the class a row's value carries.
type RowState int

const (
	RowPlain  RowState = iota // .element-value
	RowWell                   // .status-well
	RowUnwell                 // .status-unwell
	RowNote                   // .element-note
)

// DrawMail draws windows on QNTX's canvas, as a whole mail.
func DrawMail(windows ...MailWindow) (string, error) {
	l, err := qntxLook()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html>
<head><meta name="color-scheme" content="dark"><meta name="supported-color-schemes" content="dark"></head>
<body style="margin:0;padding:0;background-color:` + l.canvas + `;background-image:` + l.grid + `">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + l.canvas + `" style="background-color:` + l.canvas + `;background-image:` + l.grid + `">
<tr><td align="center" style="padding:24px 12px">
<table role="presentation" width="640" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:640px;table-layout:fixed">
<tr><td style="font-family:` + l.mono + `">
`)
	for _, w := range windows {
		l.window1(&b, w)
	}
	b.WriteString(`</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`)
	return b.String(), nil
}

func (l look) window1(b *strings.Builder, w MailWindow) {
	// Fixed, so the window is as wide as the screen and not as wide as a graph.
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + l.window + `" style="background-color:` + l.window + `;margin:0 0 ` + l.sectionGap + `;table-layout:fixed">
<tr><td style="height:` + l.barHeight + `;padding:` + l.barPadding + `;border-bottom:` + l.barBorder + `;font-family:` + l.mono + `;font-size:` + l.titleSize + `;font-weight:` + l.titleWeight + `;color:` + l.titleColor + `">`)
	if w.Symbol != "" {
		b.WriteString(html.EscapeString(w.Symbol) + `<span style="padding-left:` + l.barGap + `">` + html.EscapeString(w.Title) + `</span>`)
	} else {
		b.WriteString(html.EscapeString(w.Title))
	}
	b.WriteString(`</td></tr>
<tr><td style="padding:` + l.windowPadding + `"><div style="padding:` + l.contentPadding + `;font-family:` + l.mono + `;font-size:` + l.size + `;line-height:` + l.lineHeight + `">
`)
	for _, s := range w.Sections {
		l.section(b, s)
	}
	b.WriteString("</div></td></tr>\n</table>\n")
}

func (l look) section(b *strings.Builder, s MailSection) {
	b.WriteString(`<div style="margin-bottom:` + l.sectionGap + `">`)
	if s.Title != "" {
		b.WriteString(`<div style="margin:` + l.headingMargin + `;padding:` + l.headingPadding + `;font-size:` + l.headingSize + `;font-weight:` + l.headingWeight + `;color:` + l.headingColor + `">` + html.EscapeString(s.Title) + `</div>`)
	}
	b.WriteString(s.HTML)
	for i, r := range s.Rows {
		border := l.rowBorder
		if i == len(s.Rows)-1 {
			border = l.lastRowBorder
		}
		value := l.valueColor
		switch r.State {
		case RowWell:
			value = l.well
		case RowUnwell:
			value = l.unwell
		case RowNote:
			value = l.note
		}
		// A value with no label stands where a label would.
		align := "right"
		wrap := "word-break:break-word;overflow-wrap:anywhere"
		if r.Key {
			wrap = "word-break:" + l.keyBreak
		}
		b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-bottom:` + border + `"><tr>`)
		if r.Label != "" {
			b.WriteString(`<td valign="top" style="padding:` + l.rowPadding + `;color:` + l.labelColor + `;font-size:` + l.labelSize + `">` + html.EscapeString(r.Label) + `</td>`)
		} else {
			align = "left"
		}
		b.WriteString(`<td valign="top" align="` + align + `" style="padding:` + l.rowPadding + `;color:` + value + `;text-align:` + align + `;` + wrap + `">` + html.EscapeString(r.Value) + `</td></tr></table>`)
	}
	b.WriteString("</div>\n")
}

// MailGraph is what a graph in a mail is drawn in: the window it stands in,
// and QNTX's own rule, fill and accent.
type MailGraph struct {
	Background, Rule, Fill, Line color.NRGBA
}

// MailGraphColours reads MailGraph from web/css.
func MailGraphColours() (MailGraph, error) {
	l, err := qntxLook()
	if err != nil {
		return MailGraph{}, err
	}
	var g MailGraph
	for _, c := range []struct {
		into *color.NRGBA
		css  string
	}{{&g.Background, l.window}, {&g.Rule, l.graphRule}, {&g.Fill, l.graphFill}, {&g.Line, l.graphLine}} {
		if *c.into, err = ColorOf(c.css); err != nil {
			return MailGraph{}, errors.Wrap(err, "a graph cannot be drawn from web/css")
		}
	}
	return g, nil
}

// composite is fg laid over an opaque bg.
func composite(fg, bg color.NRGBA) color.NRGBA {
	a := float64(fg.A) / 0xff
	mix := func(f, b uint8) uint8 { return uint8(float64(f)*a + float64(b)*(1-a) + 0.5) }
	return color.NRGBA{R: mix(fg.R, bg.R), G: mix(fg.G, bg.G), B: mix(fg.B, bg.B), A: 0xff}
}

func hexOf(c color.NRGBA) string {
	const digits = "0123456789abcdef"
	out := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range []uint8{c.R, c.G, c.B} {
		out[1+2*i], out[2+2*i] = digits[v>>4], digits[v&0x0f]
	}
	return string(out)
}
