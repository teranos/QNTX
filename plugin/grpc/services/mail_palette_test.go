package services

import (
	"image/color"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "why not just use the real source"
//
// Every value a mail is drawn in is read out of web/css. A rule or token the
// mail reads that web/css stops setting fails here, naming it.
func TestAMailIsDrawnFromWebCSS(t *testing.T) {
	l, err := readLook()
	require.NoError(t, err)
	assert.Equal(t, "#2d2e36", l.canvas, "--bg-canvas, through .canvas-workspace")
	assert.Contains(t, l.grid, "repeating-linear-gradient", ".canvas-workspace's grid")
	assert.Equal(t, "1px solid #dcdedd", l.barBorder, ".title-bar, through --border")
	assert.Equal(t, "#e8e8e8", l.headingColor, ".element-section-title")
	assert.Equal(t, "1px solid #e0e0e0", l.rowBorder, ".element-row, through the fallback --panel-border-color names")
	assert.Equal(t, "none", l.lastRowBorder, ".element-row:last-child")
	assert.Equal(t, "#a9abaa", l.labelColor, ".label, through --text-on-dark-secondary")
	assert.Contains(t, l.mono, "JetBrains Mono", "--font-mono")
}

func TestASheetReadsRulesTokensAndFallbacks(t *testing.T) {
	fsys := fstest.MapFS{"a.css": {Data: []byte(`
:root { --ink: #111111; --line: 1px solid var(--ink); }
/* a comment { with a brace } */
.row, .other { color: var(--ink); border-bottom: var(--line); gap: var(--unset, 8px); }
@media (pointer: coarse) { .row { color: #999999; } }
`)}}
	s, err := readSheet(fsys, "a.css")
	require.NoError(t, err)

	for property, want := range map[string]string{"color": "#111111", "border-bottom": "1px solid #111111", "gap": "8px"} {
		got, err := s.prop(".row", property)
		require.NoError(t, err)
		assert.Equal(t, want, got, property)
	}
	other, err := s.prop(".other", "color")
	require.NoError(t, err)
	assert.Equal(t, "#111111", other)

	_, err = s.prop(".row", "padding")
	assert.ErrorContains(t, err, "no rule")
	_, err = s.resolve("var(--unset)")
	assert.ErrorContains(t, err, "names no fallback")
}

// A window's rgba is written as the one colour the eye sees on the canvas,
// for a client that reads no rgba.
func TestTheWindowIsOneColourOnTheCanvas(t *testing.T) {
	window, err := ColorOf(windowColour)
	require.NoError(t, err)
	canvas, err := ColorOf("#2d2e36")
	require.NoError(t, err)
	assert.Equal(t, "#242427", hexOf(composite(window, canvas)))
}

func TestAMailIsTablesAndInlineStyles(t *testing.T) {
	html, err := DrawMail(MailWindow{Symbol: "≡", Title: "week", Sections: []MailSection{
		{Title: "Node", Rows: []MailRow{{Label: "Restarts:", Value: "3"}, {Value: "None.", State: RowNote}}},
	}})
	require.NoError(t, err)
	assert.NotContains(t, html, "display:flex", "a mail client lays flex out its own way")
	assert.NotContains(t, html, "<style", "a mail client may drop a style block")
	assert.NotContains(t, html, "var(", "a mail client reads no CSS variables")
	assert.Contains(t, html, "Restarts:")
	assert.Equal(t, 1, strings.Count(html, "border-bottom:none"), "the last row has no rule under it")
}

func TestColourReadsHexAndRGBA(t *testing.T) {
	c, err := ColorOf("#c49a6c")
	require.NoError(t, err)
	assert.Equal(t, color.NRGBA{R: 0xc4, G: 0x9a, B: 0x6c, A: 0xff}, c)

	c, err = ColorOf("rgba(140, 110, 80, 0.25)")
	require.NoError(t, err)
	assert.Equal(t, color.NRGBA{R: 140, G: 110, B: 80, A: 64}, c)

	for _, notOne := range []string{"#c49a6", "c49a6c", "#zz9a6c", "rgba(140, 110, 80)", "rgba(140, 110, 80, 2)", "rgb(1, 2, 3)"} {
		_, err := ColorOf(notOne)
		assert.Error(t, err, notOne)
	}
}
