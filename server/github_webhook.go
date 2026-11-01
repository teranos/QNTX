package server

// Every event the GitHub App is subscribed to arrives at its one webhook. A
// push is matched against the plugins' build.* config, and QNTX builds,
// installs and restarts the plugin.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/teranos/QNTX/internal/forge"
	"io"
	"net/http"
	"slices"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// The App's webhook is at a path ROOT sets under githubWebhookPrefix, the one
// prefix the reach table opens; githubWebhookPath until ROOT sets another.
const (
	// TODO(forge): The App's webhook is one route for GitHub. A webhook per forge is routed by forge, verified by that forge's adapter, and yields forge events (Push, a pipeline finished, a change, a check).
	githubWebhookPrefix = "/github/"
	githubWebhookPath   = githubWebhookPrefix + "webhook"
)

// webhookPath is a path ROOT may set: under the prefix, and more than it.
func webhookPath(path string) error {
	rest, ok := strings.CutPrefix(path, githubWebhookPrefix)
	if !ok || rest == "" || strings.ContainsAny(rest, " ?#") {
		return errors.Newf("the webhook's path is under %s, and %q is not", githubWebhookPrefix, path)
	}
	return nil
}

// gitHubWebhook is the App's webhook secret. The route is NONE until ROOT
// generates one.
func (g *gitHub) gitHubWebhook() (string, bool) {
	if g.host.login() == nil {
		return "", false
	}
	secret, found, err := g.host.login().GitHubWebhook()
	if err != nil {
		g.host.log().Errorw("The App's webhook secret was not read, so the webhook answers nobody", "error", err)
		return "", false
	}
	return secret, found
}

// githubWebhookBody bounds a delivery; GitHub caps a payload at 25 MB.
const githubWebhookBody = 25 << 20

// TODO(forge): GitHub's push payload, read by push() below. Each forge's adapter reads its own into forge.Push.
// gitHubPush is the part of a push event a build reads.
type gitHubPush struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	Commits []struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	} `json:"commits"`
}

// TODO(forge): The adapter's. Move with gitHubPush to forge_github.go, with Host as the forge's own.
// push is what the push says in the part any forge says it. The files are
// every one GitHub says the push added, removed or modified.
func (p gitHubPush) push() forge.Push {
	var changed []string
	for _, c := range p.Commits {
		changed = slices.Concat(changed, c.Added, c.Removed, c.Modified)
	}
	return forge.Push{
		Repo:          forge.Repo{Host: forge.GitHub, Path: p.Repository.FullName},
		Ref:           p.Ref,
		After:         p.After,
		Deleted:       p.Deleted,
		Changed:       changed,
		DefaultBranch: p.Repository.DefaultBranch,
	}
}

// pushMoves is whether the push moves a source of b.
func pushMoves(p forge.Push, b pluginBuild) bool {
	for _, source := range b.sources() {
		if !source.Repo.Is(p.Repo) || source.Branch != p.Branch() {
			continue
		}
		if !source.PathNamed || p.Touched(source.Path) {
			return true
		}
	}
	return false
}

// TODO(forge): HMAC over the body under X-Hub-Signature-256 is how GitHub signs a delivery. A forge verifies a delivery its own way, in its adapter.
// signed is whether signature is body's HMAC under secret.
func signed(secret, signature string, body []byte) bool {
	hexed, ok := strings.CutPrefix(signature, "sha256=")
	if !ok {
		return false
	}
	said, err := hex.DecodeString(hexed)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(said, mac.Sum(nil))
}

// HandleGitHubWebhook takes the App's webhook deliveries.
func (g *gitHub) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "the App's webhook is POST")
		return
	}
	secret, active := g.gitHubWebhook()
	if !active {
		http.NotFound(w, r)
		return
	}
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil || r.URL.Path != settings.WebhookPath {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, githubWebhookBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the delivery did not read: "+err.Error())
		return
	}
	if !signed(secret, r.Header.Get("X-Hub-Signature-256"), body) {
		writeError(w, http.StatusUnauthorized, "the delivery is not signed with the App's webhook secret")
		return
	}
	// TODO(forge): The event names (workflow_run, push, pull_request, check_run, check_suite) are GitHub's. The handler decodes forge events, and the adapter names its own.
	event := r.Header.Get("X-GitHub-Event")
	if event == "workflow_run" {
		var run gitHubWorkflowRun
		if err := json.Unmarshal(body, &run); err != nil {
			writeError(w, http.StatusBadRequest, "the workflow_run is not readable JSON: "+err.Error())
			return
		}
		taking, err := g.host.buildsOfRun(run)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respond(w, g.host.log(), http.StatusOK, map[string][]string{"taking": taking})
		return
	}
	if event != "push" {
		// A pull request, its checks and main's CI are what approvals read
		// (ADR-052). Every other event is taken and nothing is done with it.
		touched, err := g.host.approvalsFromGitHub(r.Context(), event, body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		fixed := g.fixturesOf(r.Context(), event, body)
		respond(w, g.host.log(), http.StatusOK, map[string]any{"approvals": touched, "fixtures": fixed})
		return
	}
	var push gitHubPush
	if err := json.Unmarshal(body, &push); err != nil {
		writeError(w, http.StatusBadRequest, "the push is not readable JSON: "+err.Error())
		return
	}
	landed := push.push()
	if tag := tagLanded(landed); tag != "" {
		g.dispatchApp(tag)
	}
	names := g.host.buildsMovedBy(landed)
	filling := g.host.vaultsMovedBy(landed)
	dispatching := g.followsMovedBy(landed)
	rebuilding := g.host.cometsMovedBy(landed)
	fixed := g.fixturesOf(r.Context(), event, body)
	respond(w, g.host.log(), http.StatusOK, map[string]any{"building": names, "filling": filling, "dispatching": dispatching, "rebuilding": rebuilding, "fixtures": fixed})
}

// fixturesOf is what the node put fixtures in for one delivery, asked after the
// rest of the delivery was taken. A scanner that did not run is said in the log
// and in the answer, not as a failed delivery GitHub would hand in again.
func (g *gitHub) fixturesOf(ctx context.Context, event string, body []byte) []string {
	fixed, err := g.host.fixturesFromGitHub(ctx, event, body)
	if err != nil {
		g.host.log().Errorw("A delivery's words were not checked for what names a person", "event", event, "error", err)
		return []string{"error: " + err.Error()}
	}
	return fixed
}
