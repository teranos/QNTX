package server

import (
	"net/http"
	"strings"
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
func renderLine(items []StatusItem, p palette, clickable bool) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += "  "
		}
		colour := p.unwell
		if it.Glyph == GlyphWell {
			colour = p.well
		}

		// tmux hands the name of the clicked span back in mouse_status_range,
		// so the row is what says which span is which.
		if clickable {
			out += "#[range=user|" + rangeName(it.Name) + "]"
		}

		out += colour + it.Name + p.reset
		if it.Note != "" {
			out += " " + p.note + it.Note + p.reset
		}

		if clickable {
			out += "#[norange]"
		}
	}
	return out
}

// tmux caps a user range argument at 15 bytes. A longer name would be cut
// where tmux chose rather than where we did, and come back naming nothing.
const rangeLimit = 15

func rangeName(name string) string {
	if len(name) <= rangeLimit {
		return name
	}
	return name[:rangeLimit]
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

// Whether root_identities speaks for this admission. Middleware has already
// done the work: a session is SUPER only while admitted, and a token is refused
// when the identity that minted it stops being listed.
func rootDerived(a auth.Admission) bool {
	return a.Level == auth.LevelSuper || a.Level == auth.LevelRoot || a.Level == auth.LevelToken
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

	// What a deployment is running is not public to everyone it admits. An
	// admission root_identities does not speak for learns that QNTX is there.
	if admitted, ok := auth.AdmissionFrom(r.Context()); !ok || !rootDerived(admitted) {
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

// HandleStatusLineItem answers what one item on the row is doing, in full.
// The row has one line and cannot carry this; a click is where it goes.
// GET /statusline/{name}
func (h *StatusLineHandler) HandleStatusLineItem(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/statusline/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}

	if admitted, ok := auth.AdmissionFrom(r.Context()); !ok || !rootDerived(admitted) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such item"})
		return
	}

	if h == nil || h.registry == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such item"})
		return
	}

	// The row truncates a long name to fit tmux's range argument, so what comes
	// back is a prefix rather than the whole of it.
	health, _, _ := h.health()
	states := h.registry.GetAllStates()

	full := ""
	for _, candidate := range h.registry.List() {
		if candidate == name || rangeName(candidate) == name {
			full = candidate
			break
		}
	}
	if full == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such item", "name": name})
		return
	}

	hs := health[full]
	detail := map[string]any{
		"name":    full,
		"state":   string(states[full]),
		"healthy": hs.Healthy,
		"paused":  hs.Paused,
		"message": hs.Message,
		"details": hs.Details,
	}

	if p, ok := h.registry.Get(full); ok {
		meta := p.Metadata()
		detail["version"] = meta.Version
		detail["description"] = meta.Description
	}

	h.noteWriteFailure(writeJSON(w, http.StatusOK, detail))
}

// The same items, spelled for whichever surface asked. The write can fail and
// the caller is owed that, the same way writeJSON owes it.
func writeStatusLine(w http.ResponseWriter, format string, items []StatusItem) error {
	if format == FormatJSON {
		return writeJSON(w, http.StatusOK, StatusLineResponse{Items: items})
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	line := renderLine(items, palettes[format], format == FormatTmux)
	if _, err := w.Write([]byte(line)); err != nil {
		return errors.Wrap(err, "failed to write status line")
	}
	return nil
}
