package server

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// A deploy in flight is the whole row, and the carousels hold where they are
// until it ends. A deploy that failed stays on the row beside the rest until
// it is old or something newer reaches the row.

// SymbolDeploying is a plugin going from one build to the next.
const SymbolDeploying = "~"

// deployFailedHold is how long a failed deploy stays when nothing newer comes.
const deployFailedHold = 30 * time.Minute

// deployRevShown is how much of a rev the row names.
const deployRevShown = 7

// deployShown is one deploy as the row reads it.
type deployShown struct {
	plugin, rev       string
	replacesRevs      []string
	stage             string
	failedAt, failure string
	// at is when its stage began.
	at time.Time
}

// shown is every deploy the tracker holds, by plugin.
func (t *deployTracker) shown() []deployShown {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]deployShown, 0, len(t.deploys))
	for _, d := range t.deploys {
		out = append(out, deployShown{plugin: d.plugin, rev: d.rev, replacesRevs: slices.Clone(d.replacesRevs),
			stage: d.stage, failedAt: d.failedAt, failure: d.failure, at: d.began[d.stage]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].plugin < out[j].plugin })
	return out
}

func revShown(rev string) string {
	if len(rev) > deployRevShown {
		return rev[:deployRevShown]
	}
	return rev
}

// noDeploys is a node that holds no deploys.
func noDeploys() []deployShown { return []deployShown{} }

// firstRevShown is the first of revs as the row names it, or none.
func firstRevShown(revs []string) []string {
	for _, rev := range revs {
		return []string{revShown(rev)}
	}
	return []string{}
}

// deploysInFlight is each deploy not yet stopped at a stage, and whether there
// is one: the version the plugin runs, or the rev it was built from, then the
// rev coming in and how far.
func (h *StatusLineHandler) deploysInFlight(held []deployShown) ([]StatusItem, bool) {
	items, flying := []StatusItem{}, false
	for _, d := range held {
		if slices.Contains(deployEnded, d.stage) {
			continue
		}
		replaces := firstRevShown(d.replacesRevs)
		if p, running := h.registry.Get(d.plugin); running {
			replaces = []string{p.Metadata().Version}
		}
		note := strings.Join(append(replaces, "→ "+revShown(d.rev)+" · "+d.stage), " ")
		items, flying = append(items, StatusItem{Name: d.plugin, Note: note, Symbol: SymbolDeploying}), true
	}
	return items, flying
}

// deploysFailed is each failed deploy younger than its hold with nothing newer
// after it: no other deploy's stage, and no news left for the caller.
func (h *StatusLineHandler) deploysFailed(held []deployShown, news []News) []StatusItem {
	var items []StatusItem
	for _, d := range held {
		if d.stage != deployFailed || time.Since(d.at) >= deployFailedHold {
			continue
		}
		newer := slices.ContainsFunc(held, func(other deployShown) bool {
			return other.plugin != d.plugin && other.at.After(d.at)
		}) || slices.ContainsFunc(news, func(n News) bool {
			return n.AtMs > d.at.UnixMilli()
		})
		if newer {
			continue
		}
		firstLine := strings.SplitN(d.failure, "\n", 2)[0]
		items = append(items, StatusItem{Name: d.plugin, Note: "failed at " + d.failedAt + ": " + firstLine, Symbol: SymbolUnwell})
	}
	return items
}
