package server

// Every event the GitHub App is subscribed to arrives at its one webhook. A
// push is matched against the plugins' build.* config, and QNTX builds,
// installs and restarts the plugin.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// The App's webhook is at a path ROOT sets under githubWebhookPrefix, the one
// prefix the reach table opens; githubWebhookPath until ROOT sets another.
const (
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
func (s *QNTXServer) gitHubWebhook() (string, bool) {
	if s.authHandler == nil {
		return "", false
	}
	secret, found, err := s.authHandler.GitHubWebhook()
	if err != nil {
		s.logger.Errorw("The App's webhook secret was not read, so the webhook answers nobody", "error", err)
		return "", false
	}
	return secret, found
}

// githubWebhookBody bounds a delivery; GitHub caps a payload at 25 MB.
const githubWebhookBody = 25 << 20

// gitHubPush is the part of a push event a build reads.
type gitHubPush struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Commits []struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	} `json:"commits"`
}

// branch is the branch pushed to, or empty for a tag.
func (p gitHubPush) branch() string {
	branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
	if !ok {
		return ""
	}
	return branch
}

// touched is whether the push changed path.
func (p gitHubPush) touched(path string) bool {
	for _, c := range p.Commits {
		if slices.Contains(c.Added, path) || slices.Contains(c.Removed, path) || slices.Contains(c.Modified, path) {
			return true
		}
	}
	return false
}

// touchedUnder is whether the push changed anything inside folder.
func (p gitHubPush) touchedUnder(folder string) bool {
	for _, c := range p.Commits {
		for _, changed := range slices.Concat(c.Added, c.Removed, c.Modified) {
			if strings.HasPrefix(changed, folder+"/") {
				return true
			}
		}
	}
	return false
}

// moves is whether the push moves a source of b.
func (p gitHubPush) moves(b pluginBuild) bool {
	for _, source := range b.sources() {
		if !strings.EqualFold(source.Owner+"/"+source.Repo, p.Repository.FullName) || source.Branch != p.branch() {
			continue
		}
		if source.Path == "" || p.touched(source.Path) {
			return true
		}
	}
	return false
}

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
func (s *QNTXServer) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "the App's webhook is POST")
		return
	}
	secret, active := s.gitHubWebhook()
	if !active {
		http.NotFound(w, r)
		return
	}
	settings, err := s.nodeRecords().GitHub()
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
	if event := r.Header.Get("X-GitHub-Event"); event != "push" {
		// A pull request, its checks and main's CI are what approvals read
		// (ADR-052). Every other event is taken and nothing is done with it.
		touched, err := s.approvalsFromGitHub(r.Context(), event, body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		respond(w, s.logger, http.StatusOK, map[string][]string{"approvals": touched})
		return
	}
	var push gitHubPush
	if err := json.Unmarshal(body, &push); err != nil {
		writeError(w, http.StatusBadRequest, "the push is not readable JSON: "+err.Error())
		return
	}
	if tag := push.tagLanded(); tag != "" {
		s.dispatchApp(tag)
	}
	names := s.buildsMovedBy(push)
	filling := s.vaultsMovedBy(push)
	dispatching := s.followsMovedBy(push)
	respond(w, s.logger, http.StatusOK, map[string][]string{"building": names, "filling": filling, "dispatching": dispatching})
}

// buildsMovedBy starts the build of every enabled plugin push moves, and names them.
func (s *QNTXServer) buildsMovedBy(push gitHubPush) []string {
	logger := s.logger.Named("build")
	records, err := s.pluginRecords().Plugins()
	if err != nil {
		logger.Errorw("A push arrived and the plugin records were not read", "repo", push.Repository.FullName, "error", err)
		return []string{}
	}
	names := []string{}
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		b, built, err := buildOf(record)
		if err != nil {
			logger.Errorw("A plugin's build is not one QNTX can run", "plugin", record.Name, "error", err)
			continue
		}
		if !built || !push.moves(b) {
			continue
		}
		names = append(names, b.name)
		logger.Infow("A push moves a plugin's source", "plugin", b.name, "repo", push.Repository.FullName, "ref", push.Ref, "after", push.After)
		sacred.Go("plugin.build."+b.name, func() {
			s.building.Lock()
			defer s.building.Unlock()
			s.buildIfMoved(s.lifetime(), b, logger)
		})
	}
	return names
}
