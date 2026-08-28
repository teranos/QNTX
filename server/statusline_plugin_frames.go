package server

import (
	"strconv"
	"strings"
)

// A plugin's slot on the row used to hold its version forever. A version is a
// near-constant — capy sat at 0.244.0 while every one of its handlers failed
// hourly — so the slot spent its characters on the one thing that never moved.

// Handler names are namespaced by the plugin that declared them, so the prefix
// is what says whose a handler is: capy/capy.campaigns belongs to capy.
const handlerNamespaceSep = "/"

// How much more often a failing handler is drawn than a working one. Failures
// are the reason to look at the row at all.
const failingHandlerWeight = 3

// pluginOf is the plugin a handler was declared by, or "" for a handler with no
// namespace — a built-in, which belongs to no plugin's slot.
func pluginOf(handler string) string {
	name, _, found := strings.Cut(handler, handlerNamespaceSep)
	if !found {
		return ""
	}
	return name
}

// handlersOf is the handlers a plugin declared, in the order the registry holds
// them so the rotation is stable across redraws.
func handlersOf(all []string, plugin string) []string {
	out := make([]string, 0, len(all))
	for _, handler := range all {
		if pluginOf(handler) == plugin {
			out = append(out, handler)
		}
	}
	return out
}

// pluginFrames is what one plugin's slot rotates through: every handler it
// failed on, weighted to come round more often, then its version, then the
// handlers that are working.
func pluginFrames(plugin, version string, handlers []string, failures []handlerFailureRun) []StatusItem {
	failed := make(map[string]handlerFailureRun, len(failures))
	for _, run := range failures {
		if pluginOf(run.Handler) == plugin {
			failed[run.Handler] = run
		}
	}

	frames := make([]StatusItem, 0, len(handlers)+1)

	// Failing first and repeated: a handler that is broken should be the thing
	// the slot is most often showing.
	for _, handler := range handlers {
		run, broken := failed[handler]
		if !broken {
			continue
		}
		item := handlerFailureItemsFor([]handlerFailureRun{run})[0]
		for i := 0; i < failingHandlerWeight; i++ {
			frames = append(frames, item)
		}
	}

	// A failure recorded for a handler the registry no longer lists still
	// belongs to this plugin, and losing it would hide the fire.
	for _, run := range failures {
		if pluginOf(run.Handler) != plugin {
			continue
		}
		if _, listed := failed[run.Handler]; listed && containsHandler(handlers, run.Handler) {
			continue
		}
		frames = append(frames, handlerFailureItemsFor([]handlerFailureRun{run})[0])
	}

	frames = append(frames, StatusItem{Name: plugin, Note: version, Glyph: GlyphWell})

	for _, handler := range handlers {
		if _, broken := failed[handler]; broken {
			continue
		}
		frames = append(frames, StatusItem{
			Name:  plugin,
			Note:  shortHandler(handler),
			Glyph: GlyphWell,
		})
	}

	return frames
}

func containsHandler(handlers []string, want string) bool {
	for _, handler := range handlers {
		if handler == want {
			return true
		}
	}
	return false
}

// shortHandler drops the namespace the slot already carries in its name.
// capy/capy.campaigns under a slot named capy reads as capy.campaigns.
func shortHandler(handler string) string {
	_, rest, found := strings.Cut(handler, handlerNamespaceSep)
	if !found {
		return handler
	}
	return rest
}

// pluginItem is the frame this plugin's slot is showing. A plugin that declared
// no handlers has only its version to say, which is the old behaviour.
func pluginItem(at int, plugin, version string, healthy bool, handlers []string, failures []handlerFailureRun) StatusItem {
	frames := pluginFrames(plugin, version, handlers, failures)
	if len(frames) == 0 {
		return StatusItem{Name: plugin, Note: version, Glyph: glyphFor(healthy)}
	}

	item := frames[((at%len(frames))+len(frames))%len(frames)]

	// The plugin's own health outranks the frame: a plugin reporting unwell is
	// unwell whichever of its handlers the slot happens to be showing.
	if !healthy {
		item.Glyph = GlyphUnwell
	}
	return item
}

func glyphFor(healthy bool) string {
	if healthy {
		return GlyphWell
	}
	return GlyphUnwell
}

// countedHandlers is what a plugin says when it has handlers but the row has no
// room to name them one at a time.
func countedHandlers(handlers []string) string {
	return strconv.Itoa(len(handlers)) + " handlers"
}
