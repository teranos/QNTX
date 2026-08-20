package server

import (
	"net/http"
	"time"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// The glyph is the whole of what crosses; each surface maps it to its own colour.
const (
	GlyphWell   = "+"
	GlyphUnwell = "!"
)

// StatusItem is one thing worth naming, with what it is at and how it is.
type StatusItem struct {
	Name  string `json:"name"`
	Note  string `json:"note,omitempty"`
	Glyph string `json:"glyph"`
}

// StatusLineResponse carries the items in the order they should be read.
type StatusLineResponse struct {
	Items []StatusItem `json:"items"`
}

// A surface draws in its own escapes and cannot read another's: tmux prints
// ANSI literally, a terminal prints #[fg=...] literally. So the caller names
// what it draws for, and there is no default.
const (
	FormatJSON = "json"
	FormatANSI = "ansi"
	FormatTmux = "tmux"
)

// Escapes for one surface. The glyph stays the whole of what crosses; this is
// only how it is spelled.
type palette struct {
	well   string
	unwell string
	note   string
	reset  string
}

var palettes = map[string]palette{
	FormatANSI: {well: "\033[32m", unwell: "\033[31m", note: "\033[2m", reset: "\033[0m"},
	FormatTmux: {well: "#[fg=colour34]", unwell: "#[fg=colour160]", note: "#[fg=colour244]", reset: "#[default]"},
}

// One line, whatever the items are. tmux keeps the first line of a #() and
// drops the rest, so a row that wrapped would lose everything past the first
// newline without saying so.
func renderLine(items []StatusItem, p palette) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += "  "
		}
		colour := p.unwell
		if it.Glyph == GlyphWell {
			colour = p.well
		}
		out += colour + it.Name + p.reset
		if it.Note != "" {
			out += " " + p.note + it.Note + p.reset
		}
	}
	return out
}

// StatusLineHandler answers what the surfaces draw.
type StatusLineHandler struct {
	registry *plugin.Registry
	logger   *zap.SugaredLogger
	health   func() (map[string]plugin.HealthStatus, time.Time, string)
}

// NewStatusLineHandler builds the handler behind /statusline.
func NewStatusLineHandler(registry *plugin.Registry, logger *zap.SugaredLogger,
	health func() (map[string]plugin.HealthStatus, time.Time, string)) *StatusLineHandler {
	return &StatusLineHandler{registry: registry, logger: logger, health: health}
}

// Running and reporting healthy. Healthy while stopped is not doing anything.
func well(healthy bool, state string) bool {
	return healthy && state == "running"
}

// Whether root_identities speaks for this caller. Middleware has already done
// the work: a session is SUPER only while admitted, and a token is refused when
// the identity that minted it stops being listed.
func rootDerived(c auth.Caller) bool {
	return c.Level == auth.LevelSuper || c.Level == auth.LevelRoot || c.Level == auth.LevelToken
}

// HandleStatusLine returns the items a status line draws, unwell first.
// GET /statusline
func (h *StatusLineHandler) HandleStatusLine(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Named, never guessed. A caller that omits it is told what it may ask for
	// rather than handed a row its surface cannot render.
	format := r.URL.Query().Get("format")
	if format != FormatJSON && format != FormatANSI && format != FormatTmux {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "format is required",
			"formats": []string{FormatJSON, FormatANSI, FormatTmux},
		})
		return
	}

	items := make([]StatusItem, 0)

	// What a deployment is running is not public to everyone it admits. A caller
	// who is not spoken for by root_identities learns that QNTX is there.
	if caller, ok := auth.CallerFrom(r.Context()); !ok || !rootDerived(caller) {
		h.noteWriteFailure(writeStatusLine(w, format, []StatusItem{{Name: "QNTX", Glyph: GlyphWell}}))
		return
	}

	if h == nil || h.registry == nil {
		h.noteWriteFailure(writeStatusLine(w, format, items))
		return
	}

	// The last probe, not a fresh one. Probing per request cost gRPC calls and
	// held the registry's read lock across them.
	healthResults, _, _ := h.health()
	stateResults := h.registry.GetAllStates()

	type entry struct {
		item StatusItem
		well bool
	}
	entries := make([]entry, 0)

	seen := make(map[string]bool)
	for _, name := range h.registry.List() {
		seen[name] = true
		p, ok := h.registry.Get(name)
		if !ok {
			continue
		}
		meta := p.Metadata()
		healthy := well(healthResults[name].Healthy, string(stateResults[name]))
		glyph := GlyphUnwell
		if healthy {
			glyph = GlyphWell
		}
		entries = append(entries, entry{
			item: StatusItem{Name: meta.Name, Note: meta.Version, Glyph: glyph},
			well: healthy,
		})
	}

	// A plugin that never loaded has no metadata, so it is named and left at that.
	for _, name := range h.registry.ListEnabled() {
		if seen[name] {
			continue
		}
		entries = append(entries, entry{
			item: StatusItem{Name: name, Glyph: GlyphUnwell},
			well: false,
		})
	}

	// Two passes rather than a sort: the order is unwell then well.
	for _, wantWell := range []bool{false, true} {
		for _, e := range entries {
			if e.well == wantWell {
				items = append(items, e.item)
			}
		}
	}

	h.noteWriteFailure(writeStatusLine(w, format, items))
}

// A response that did not reach the client is not a response. There is no
// client left to tell, so it goes where the operator can find it.
func (h *StatusLineHandler) noteWriteFailure(err error) {
	if err == nil || h == nil || h.logger == nil {
		return
	}
	h.logger.Errorw("status line not written", "error", err)
}

// The same items, spelled for whichever surface asked. The write can fail and
// the caller is owed that, the same way writeJSON owes it.
func writeStatusLine(w http.ResponseWriter, format string, items []StatusItem) error {
	if format == FormatJSON {
		return writeJSON(w, http.StatusOK, StatusLineResponse{Items: items})
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(renderLine(items, palettes[format]))); err != nil {
		return errors.Wrap(err, "failed to write status line")
	}
	return nil
}
