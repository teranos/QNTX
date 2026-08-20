package server

import (
	"net/http"
	"time"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
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

	items := make([]StatusItem, 0)

	// What a deployment is running is not public to everyone it admits. A caller
	// who is not spoken for by root_identities learns that QNTX is there.
	if caller, ok := auth.CallerFrom(r.Context()); !ok || !rootDerived(caller) {
		writeJSON(w, http.StatusOK, StatusLineResponse{
			Items: []StatusItem{{Name: "QNTX", Glyph: GlyphWell}},
		})
		return
	}

	if h == nil || h.registry == nil {
		writeJSON(w, http.StatusOK, StatusLineResponse{Items: items})
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

	writeJSON(w, http.StatusOK, StatusLineResponse{Items: items})
}
