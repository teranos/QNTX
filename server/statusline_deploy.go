package server

// Each plugin's newest deploy, as the node holds it (plugin_deploy.go), sent
// with the row as data. How it is drawn is the surface's to decide.
// "just give me the data and i decide how to render it"

import (
	"maps"
	"slices"
	"sort"
	"time"
)

// DeployData is one plugin's newest deploy.
type DeployData struct {
	Plugin string `json:"plugin"`
	// Stage is where it is, or where it stopped.
	Stage string `json:"stage"`
	// Repo and Pushed: a deploy a push began, at a commit checks run on.
	Repo   string `json:"repo,omitempty"`
	Pushed bool   `json:"pushed"`
	Rev    string `json:"rev"`
	// Checks is what each GitHub Actions check on the commit pushed last said,
	// by its name: waiting, running, done or failed.
	Checks map[string]string `json:"checks"`
	// Version is what the build says it is; there is none before the build.
	Version string `json:"version,omitempty"`
	// Began is when each stage it reached began.
	Began    map[string]time.Time `json:"began"`
	FailedAt string               `json:"failed_at,omitempty"`
	Failure  string               `json:"failure,omitempty"`
}

// deployData is every deploy the node holds, by plugin, and whether one is
// in flight: the carousel waits while one is.
func deployData(deploys []pluginDeploy) ([]DeployData, bool) {
	sort.Slice(deploys, func(i, j int) bool { return deploys[i].plugin < deploys[j].plugin })
	out := make([]DeployData, 0, len(deploys))
	inFlight := false
	for _, d := range deploys {
		if !slices.Contains(deployEnded, d.stage) {
			inFlight = true
		}
		data := DeployData{
			Plugin: d.plugin, Stage: d.stage, Repo: d.repo, Pushed: d.pushed, Rev: d.rev,
			Checks: maps.Clone(d.checks), Began: maps.Clone(d.began),
			FailedAt: d.failedAt, Failure: d.failure,
		}
		if d.versioned {
			data.Version = d.version
		}
		out = append(out, data)
	}
	return out, inFlight
}
