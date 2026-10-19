package statusline

// Each plugin's newest deploy, as the node holds it (server/plugin_deploy.go), sent
// with the row as data. How it is drawn is the surface's to decide.
// "just give me the data and i decide how to render it"

import "time"

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
