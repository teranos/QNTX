//go:build !atsless

package server

import (
	"strings"

	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
)

// buildPerformanceData converts the slow log collector's rolling history
// into a JSON-friendly structure for the frontend.
func buildPerformanceData() map[string]any {
	snap := sqlitecgo.GetPerformanceSnapshot()
	if snap.Current == nil {
		return nil
	}

	// Current window: operations sorted by variance (max-min spread)
	type opEntry struct {
		name     string
		stats    *sqlitecgo.BucketStats
		variance float64
	}
	var ops []opEntry
	for name, stats := range snap.Current {
		spread := stats.Max - stats.Min
		variance := float64(spread) / float64(stats.Avg+1) // relative variance
		ops = append(ops, opEntry{name, stats, variance})
	}
	// Sort by variance descending
	for i := 0; i < len(ops); i++ {
		for j := i + 1; j < len(ops); j++ {
			if ops[j].variance > ops[i].variance {
				ops[i], ops[j] = ops[j], ops[i]
			}
		}
	}

	var current []map[string]any
	for _, op := range ops {
		kind := "op"
		name := op.name
		if strings.HasPrefix(name, "mutex:") {
			kind = "mutex"
			name = strings.TrimPrefix(name, "mutex:")
		}
		current = append(current, map[string]any{
			"name":  name,
			"kind":  kind,
			"count": op.stats.Count,
			"min":   op.stats.Min.Milliseconds(),
			"max":   op.stats.Max.Milliseconds(),
			"avg":   op.stats.Avg.Milliseconds(),
		})
	}

	// History: per-operation avg over time (for sparklines)
	// Collect all operation names seen across history
	allOps := make(map[string]bool)
	for _, window := range snap.History {
		for name := range window {
			allOps[name] = true
		}
	}

	sparklines := make(map[string][]any)
	for name := range allOps {
		series := make([]any, len(snap.History))
		for i, window := range snap.History {
			if stats, ok := window[name]; ok {
				series[i] = stats.Avg.Milliseconds()
			} else {
				series[i] = nil
			}
		}
		sparklines[name] = series
	}

	return map[string]any{
		"current":    current,
		"sparklines": sparklines,
		"windows":    len(snap.History),
	}
}
