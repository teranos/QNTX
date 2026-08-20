package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A caller that names no format is told what it may ask for. Guessing here
// hands a surface escapes it prints literally.
func TestStatusLineRequiresFormat(t *testing.T) {
	var h *StatusLineHandler

	for _, q := range []string{"", "?format=", "?format=html"} {
		req := httptest.NewRequest(http.MethodGet, "/statusline"+q, nil)
		rec := httptest.NewRecorder()
		h.HandleStatusLine(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: got %d, want %d", q, rec.Code, http.StatusBadRequest)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%q: body is not json: %v", q, err)
		}
		if body["formats"] == nil {
			t.Fatalf("%q: the refusal does not say what may be asked for", q)
		}
	}
}

var sample = []StatusItem{
	{Name: "capy", Note: "0.244.0", Glyph: GlyphWell},
	{Name: "duif", Glyph: GlyphUnwell},
}

// Each surface gets its own escapes, and neither can read the other's.
func TestRenderLineSpellsEachSurface(t *testing.T) {
	ansi := renderLine(sample, palettes[FormatANSI], false)
	if !strings.Contains(ansi, "\033[32mcapy\033[0m") {
		t.Fatalf("ansi well item not in ansi escapes: %q", ansi)
	}
	if !strings.Contains(ansi, "\033[31mduif\033[0m") {
		t.Fatalf("ansi unwell item not in ansi escapes: %q", ansi)
	}
	if strings.Contains(ansi, "#[") {
		t.Fatalf("ansi carries tmux markup: %q", ansi)
	}

	tm := renderLine(sample, palettes[FormatTmux], false)
	if !strings.Contains(tm, "#[fg=colour34]capy#[default]") {
		t.Fatalf("tmux well item not in tmux markup: %q", tm)
	}
	if !strings.Contains(tm, "#[fg=colour160]duif#[default]") {
		t.Fatalf("tmux unwell item not in tmux markup: %q", tm)
	}
	if strings.Contains(tm, "\033") {
		t.Fatalf("tmux carries ansi escapes: %q", tm)
	}
}

// A note rides with its item, and an item without one draws no stray space.
func TestRenderLineNote(t *testing.T) {
	line := renderLine(sample, palettes[FormatANSI], false)
	if !strings.Contains(line, "\033[2m0.244.0\033[0m") {
		t.Fatalf("note not dim: %q", line)
	}
	if strings.Contains(line, "duif\033[0m \033[2m\033[0m") {
		t.Fatalf("an absent note drew a space and an empty span: %q", line)
	}
}

// tmux keeps the first line of a #() and drops the rest, so a row that wrapped
// would lose everything past the newline without saying so.
func TestRenderLineIsOneLine(t *testing.T) {
	for name, p := range palettes {
		line := renderLine(sample, p, true)
		if strings.ContainsAny(line, "\n\r") {
			t.Fatalf("%s: row is not one line: %q", name, line)
		}
	}
}

// A clickable row wraps each item in a range tmux can name back, and the row
// that is not clickable carries no markers at all.
func TestRenderLineRanges(t *testing.T) {
	on := renderLine(sample, palettes[FormatTmux], true)
	if !strings.Contains(on, "#[range=user|capy]") {
		t.Fatalf("no range for capy: %q", on)
	}
	if !strings.Contains(on, "#[range=user|duif]") {
		t.Fatalf("no range for duif: %q", on)
	}
	if strings.Count(on, "#[norange]") != len(sample) {
		t.Fatalf("every range must be closed: %q", on)
	}

	off := renderLine(sample, palettes[FormatTmux], false)
	if strings.Contains(off, "range=") {
		t.Fatalf("markers on a row nobody can click: %q", off)
	}
}

// tmux caps a user range argument at 15 bytes, so a longer name is cut here
// rather than by tmux, and it is cut the same way on the way back.
func TestRangeNameFitsTmux(t *testing.T) {
	if got := rangeName("capy"); got != "capy" {
		t.Fatalf("short name changed: %q", got)
	}

	long := "averyveryverylongpluginname"
	got := rangeName(long)
	if len(got) != rangeLimit {
		t.Fatalf("long name is %d bytes, want %d: %q", len(got), rangeLimit, got)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatalf("the cut is not a prefix of the name: %q", got)
	}
}

// Nothing to say is nothing drawn, not an empty pair of escapes.
func TestRenderLineEmpty(t *testing.T) {
	for name, p := range palettes {
		if got := renderLine(nil, p, true); got != "" {
			t.Fatalf("%s: empty items drew %q", name, got)
		}
	}
}
