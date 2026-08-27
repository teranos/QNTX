package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/version"
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
	// The watcher store, fetched per request because a backend supplies it
	// after this handler is built. Nil draws no failure items.
	watchers func() storage.Watchers
	// Which frame the rotating slot is on. The node holds it, so every surface
	// drawing the row sees the same one.
	carousel *carousel
	// What the rotating slot asks about. Nil draws no rotating slot at all.
	node StatusLineNode
}

// NewStatusLineHandler builds the handler behind /statusline.
func NewStatusLineHandler(registry *plugin.Registry, logger *zap.SugaredLogger,
	health func() (map[string]plugin.HealthStatus, time.Time, string),
	watchers func() storage.Watchers, node StatusLineNode) *StatusLineHandler {
	return &StatusLineHandler{
		registry: registry,
		logger:   logger,
		health:   health,
		watchers: watchers,
		carousel: newCarousel(),
		node:     node,
	}
}

// StatusLineNode is what the rotating slot asks the node about. An interface
// because the row has to be drawable in a test with no server behind it.
type StatusLineNode interface {
	// Uptime is how long this process has been answering.
	Uptime() time.Duration
	// ParserVersion is the ats WASM module's own version.
	ParserVersion() string
	// Pressure is the last sampled CPU and memory percentages. Sampling CPU
	// blocks for a second, so these are read from the node's cache.
	Pressure() (cpuPct, memPct float64, ok bool)
	// Attestations is the count from that same cache.
	Attestations() (int, bool)
	Watchers() int
	Schedules() int
	Handlers() int
	// Refusals is how many callers this process turned away, and how many of
	// those held a token.
	Refusals() (turnedAway, stale int64)
}

// A frame is produced only when it is the one being drawn. The row is polled
// constantly and shows one frame at a time, so building all of them per request
// would pay for nine nobody sees.
type carouselFrame struct {
	produce func(StatusLineNode) StatusItem
	// omit says this frame has nothing to report. A row is a handful of
	// characters, and a slot spent saying nothing happened is a slot not
	// spent on what did. Nil is a frame that is always worth drawing.
	omit func(StatusLineNode) bool
}

// The order the slot sweeps. What the node is, then how hard it is working,
// then how much it holds.
var carouselFrames = []carouselFrame{
	{produce: func(StatusLineNode) StatusItem {
		build := version.Get()
		return StatusItem{Name: build.Version, Note: build.Short(), Glyph: GlyphWell}
	}},
	{produce: func(n StatusLineNode) StatusItem {
		return StatusItem{Name: "up", Note: shortDuration(n.Uptime()), Glyph: GlyphWell}
	}},
	{produce: func(n StatusLineNode) StatusItem {
		return countItem("ats", n.ParserVersion())
	}},
	{produce: func(n StatusLineNode) StatusItem {
		cpu, _, ok := n.Pressure()
		return pctItem("cpu", cpu, ok)
	}},
	{produce: func(n StatusLineNode) StatusItem {
		_, mem, ok := n.Pressure()
		return pctItem("mem", mem, ok)
	}},
	{produce: func(n StatusLineNode) StatusItem {
		held, ok := n.Attestations()
		if !ok {
			return StatusItem{Name: "attestations", Note: "uncounted", Glyph: GlyphUnwell}
		}
		return countItem("attestations", strconv.Itoa(held))
	}},
	{produce: func(n StatusLineNode) StatusItem {
		return countItem("watchers", strconv.Itoa(n.Watchers()))
	}},
	{produce: func(n StatusLineNode) StatusItem {
		return countItem("schedules", strconv.Itoa(n.Schedules()))
	}},
	{produce: func(n StatusLineNode) StatusItem {
		return countItem("handlers", strconv.Itoa(n.Handlers()))
	}},
	{
		produce: func(n StatusLineNode) StatusItem {
			return refusedItem(n.Refusals())
		},
		// Nobody turned away is the ordinary state and needs no line.
		omit: func(n StatusLineNode) bool {
			turnedAway, _ := n.Refusals()
			return turnedAway == 0
		},
	},
}

// What the node turned away. Unwell is reserved for the ones holding a token:
// a person refused signs in a second later, a machine refused keeps presenting
// the same dead credential until somebody replaces it.
func refusedItem(turnedAway, stale int64) StatusItem {
	if stale > 0 {
		return StatusItem{
			Name:  "refused",
			Note:  strconv.FormatInt(turnedAway, 10) + ", " + strconv.FormatInt(stale, 10) + " holding a token",
			Glyph: GlyphUnwell,
		}
	}
	return StatusItem{Name: "refused", Note: strconv.FormatInt(turnedAway, 10), Glyph: GlyphWell}
}

// A named thing and what it is at. Empty is unwell: a value the node could not
// produce says so rather than drawing a blank where a number belongs.
func countItem(name, note string) StatusItem {
	if note == "" {
		return StatusItem{Name: name, Note: "unknown", Glyph: GlyphUnwell}
	}
	return StatusItem{Name: name, Note: note, Glyph: GlyphWell}
}

func pctItem(name string, pct float64, ok bool) StatusItem {
	if !ok {
		return StatusItem{Name: name, Note: "unsampled", Glyph: GlyphUnwell}
	}
	return StatusItem{Name: name, Note: strconv.Itoa(int(pct)) + "%", Glyph: GlyphWell}
}

// Days and hours, or hours and minutes, or minutes. A status line has room for
// two units and no room for a decimal.
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return strconv.Itoa(days) + "d" + strconv.Itoa(hours) + "h"
	}
	if hours > 0 {
		return strconv.Itoa(hours) + "h" + strconv.Itoa(minutes) + "m"
	}
	return strconv.Itoa(minutes) + "m"
}

// The one frame the row is showing. A handler with no node behind it draws
// nothing here rather than guessing at values it cannot read.
func (h *StatusLineHandler) carouselItem() []StatusItem {
	if h == nil || h.carousel == nil || h.node == nil {
		return nil
	}
	// The sweep keeps its own pace; a frame with nothing to say is stepped past
	// rather than drawn blank, so the slot never stutters.
	at := h.carousel.frame(len(carouselFrames))
	for step := range carouselFrames {
		frame := carouselFrames[(at+step)%len(carouselFrames)]
		if frame.omit != nil && frame.omit(h.node) {
			continue
		}
		return []StatusItem{frame.produce(h.node)}
	}
	return nil
}

// Who the row is drawn for, leftmost. ADR-027 is that the level says what and
// the User says who, so the row answers both rather than picking one.

// The display_name when there is one. A route is a way in rather than a person,
// and it falls back to one only where nothing records a User (ADR-031).
func callerItem(a auth.Admission) StatusItem {
	// Never the route. A profile URL is a door, not a person, and putting one
	// on the row says who let you in rather than who you are.
	name := a.DisplayName
	if name == "" {
		name = "QNTX"
	}
	return StatusItem{Name: name, Note: string(a.Level), Glyph: GlyphWell}
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

	admitted, ok := auth.AdmissionFrom(r.Context())

	// What a deployment is running is not public to everyone it admits. An
	// admission root_identities does not speak for learns that QNTX is there.
	if !ok || !rootDerived(admitted) {
		h.noteWriteFailure(writeStatusLine(w, format, []StatusItem{{Name: "QNTX", Glyph: GlyphWell}}))
		return
	}

	// Who is looking is pinned leftmost and the plugins are pinned right; the
	// rotating slot sits between them.
	items := append([]StatusItem{callerItem(admitted)}, h.carouselItem()...)

	// A failing handler is a fix-now kind of event: failures lead the row,
	// ahead of everything the plugin registry has to say.
	items = append(items, watcherFailureItemsFor(h.recentWatcherFailures(r.Context()))...)

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

	// Failures outrank the registry here the way they do on the row: a name
	// that matches a failing watcher answers with the failure in full.
	if detail, ok := h.watcherFailureDetail(r.Context(), name); ok {
		h.noteWriteFailure(writeJSON(w, http.StatusOK, detail))
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
