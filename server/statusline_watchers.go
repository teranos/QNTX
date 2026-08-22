package server

import (
	"context"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats/storage"
)

// A failing handler is a fix-now kind of event, so failures outrank plugin
// versions on the row: they are drawn first, before everything the registry
// has to say. The row names what is failing and how long ago; the detail is
// one click away at /statusline/{name}.

const (
	// The window failures stay on the row. The first hour is when a handler's
	// failures matter most; past it, the fire history still holds them.
	watcherFailureWindow = time.Hour
	// The row is one line; the newest failures name the fire.
	watcherFailureItems = 3
)

// watcherErrorSource is what the row asks of a watcher store. Asserted, not
// declared on the Watchers interface — a backend without it draws no failure
// items.
type watcherErrorSource interface {
	RecentErrorFires(ctx context.Context, sinceMs int64, limit int) ([]storage.WatcherErrorFire, error)
}

// recentWatcherFailures reads the failing watchers inside the window, or
// nothing when no store is wired or the store cannot answer.
func (h *StatusLineHandler) recentWatcherFailures(ctx context.Context) []storage.WatcherErrorFire {
	if h == nil || h.watchers == nil {
		return nil
	}
	src, ok := h.watchers().(watcherErrorSource)
	if !ok {
		return nil
	}
	since := time.Now().Add(-watcherFailureWindow).UnixMilli()
	fires, err := src.RecentErrorFires(ctx, since, watcherFailureItems)
	if err != nil {
		// The row still draws without them; the failure to read failures is
		// itself worth finding.
		if h.logger != nil {
			h.logger.Errorw("watcher failures not read for status line", "error", err)
		}
		return nil
	}
	return fires
}

// watcherFailureItemsFor spells failures as row items: the watcher's name,
// how long ago it last failed, unwell.
func watcherFailureItemsFor(fires []storage.WatcherErrorFire) []StatusItem {
	items := make([]StatusItem, 0, len(fires))
	for _, f := range fires {
		name := f.Name
		if name == "" {
			name = f.WatcherID
		}
		items = append(items, StatusItem{
			Name:  name,
			Note:  shortAgo(time.Since(time.UnixMilli(f.AtMs))),
			Glyph: GlyphUnwell,
		})
	}
	return items
}

// watcherFailureDetail answers /statusline/{name} for a failing watcher: the
// latest error in full, and when. Matches the same truncated-name rule the
// plugin path uses, since tmux click ranges shorten what they carry.
func (h *StatusLineHandler) watcherFailureDetail(ctx context.Context, name string) (map[string]any, bool) {
	for _, f := range h.recentWatcherFailures(ctx) {
		candidate := f.Name
		if candidate == "" {
			candidate = f.WatcherID
		}
		if candidate != name && rangeName(candidate) != name {
			continue
		}
		return map[string]any{
			"name":       candidate,
			"watcher_id": f.WatcherID,
			"healthy":    false,
			"failed_at":  time.UnixMilli(f.AtMs).UTC().Format(time.RFC3339),
			"error":      f.Error,
		}, true
	}
	return nil, false
}

// One unit, no decimals: 4s, 2m, 1h. The row has no room for more and the
// exact moment is in the detail.
func shortAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
}
