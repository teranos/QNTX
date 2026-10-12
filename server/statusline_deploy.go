package server

// A plugin's deploy, relayed to the row from what the node holds of it
// (plugin_deploy.go). While one is underway it leads the row, and the carousel
// waits until the plugin is live.

import (
	"slices"
	"sort"
	"time"
)

// DeployBar is one progress bar of a deploy, drawn as the background behind
// the plugin's name: filled Done of Of of the way, or failed.
type DeployBar struct {
	// Kind is which: GitHub's checks, or the node's own build.
	Kind   string `json:"kind"`
	Done   int    `json:"done"`
	Of     int    `json:"of"`
	Failed bool   `json:"failed,omitempty"`
}

// deployOrder is how far along the node a deploy is.
var deployOrder = []string{deployQueued, deployBuilding, deployRestarting, deployLive}

// deployRow is what the row draws of the deploys, and whether one is underway.
type deployRow struct {
	items    []StatusItem
	underway bool
}

// deployItems is each deploy the row draws now: every one underway, and a
// failed one while it is still the news.
func deployItems(deploys []pluginDeploy, now time.Time, news []News) deployRow {
	sort.Slice(deploys, func(i, j int) bool { return deploys[i].plugin < deploys[j].plugin })

	row := deployRow{items: []StatusItem{}}
	for _, d := range deploys {
		if d.stage == deployFailed {
			if !failureStands(d, deploys, news, now) {
				continue
			}
		} else if slices.Contains(deployEnded, d.stage) {
			continue
		} else {
			row.underway = true
		}
		row.items = append(row.items, deployItem(d))
	}
	return row
}

// failureStands is whether a failed deploy is still the news. Time clears it,
// and so does other news: news left for the caller, or another deploy beginning.
func failureStands(failed pluginDeploy, deploys []pluginDeploy, news []News, now time.Time) bool {
	at := failed.began[deployFailed]
	if !now.Before(at.Add(newsHold)) {
		return false
	}
	for _, n := range news {
		if n.AtMs > at.UnixMilli() {
			return false
		}
	}
	for _, d := range deploys {
		if d.plugin == failed.plugin {
			continue
		}
		for _, began := range d.began {
			if began.After(at) {
				return false
			}
		}
	}
	return true
}

// deployItem is one deploy: the plugin, the version coming in, the checks on
// the commit pushed, and how far the node is.
func deployItem(d pluginDeploy) StatusItem {
	item := StatusItem{Name: d.plugin, Symbol: SymbolWell}
	// The version coming in, once its build said it; ug alternates it with the
	// name. Before the build there is no version to say.
	if d.versioned {
		item.Note = d.version
	}

	// A deploy no push began has no commit for checks to run on.
	if d.pushed {
		passed, failed := 0, false
		for _, state := range d.checks {
			switch state {
			case checkDone:
				passed++
			case checkFailed:
				failed = true
			}
		}
		item.Bars = append(item.Bars, DeployBar{Kind: barChecks, Done: passed, Of: len(d.checks), Failed: failed})
	}

	of := len(deployOrder) - 1
	if d.stage == deployFailed {
		item.Symbol = SymbolUnwell
		item.Bars = append(item.Bars, DeployBar{Kind: barNode, Done: max(slices.Index(deployOrder, d.failedAt), 0), Of: of, Failed: true})
		return item
	}
	// Waiting on the checks, the node's build has not begun.
	item.Bars = append(item.Bars, DeployBar{Kind: barNode, Done: max(slices.Index(deployOrder, d.stage), 0), Of: of})
	return item
}

// The kinds of bar a deploy has.
const (
	barChecks = "checks"
	barNode   = "node"
)
