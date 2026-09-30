package server

// A push arrives at the App's webhook, QNTX matches it against the plugins'
// build.* config, and builds, installs and restarts the plugin.

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
)

// githubPushPath is the App's webhook URL on the node.
const githubPushPath = "/github/push"

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

// githubPushBody bounds a delivery; GitHub caps a payload at 25 MB.
const githubPushBody = 25 << 20

// gitHubPush is the part of a push event a build reads.
type gitHubPush struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
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

// HandleGitHubPush takes the App's webhook deliveries.
func (s *QNTXServer) HandleGitHubPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "the App's webhook is POST")
		return
	}
	secret, active := s.gitHubWebhook()
	if !active {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, githubPushBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the delivery did not read: "+err.Error())
		return
	}
	if !signed(secret, r.Header.Get("X-Hub-Signature-256"), body) {
		writeError(w, http.StatusUnauthorized, "the delivery is not signed with the App's webhook secret")
		return
	}
	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var push gitHubPush
	if err := json.Unmarshal(body, &push); err != nil {
		writeError(w, http.StatusBadRequest, "the push is not readable JSON: "+err.Error())
		return
	}
	names := s.buildsMovedBy(push)
	respond(w, s.logger, http.StatusOK, map[string][]string{"building": names})
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
