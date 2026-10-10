package server

import (
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
)

// What carries a push from this node (ADR-048, Its git): a token the node
// mints as the GitHub App, for one repository, when git asks for it.

// A route and no sigil, and no tool (routeTool): on the box the agent took the
// sigil as a tool, and the token went into its context and into a command.
// What asks here is git's credential helper (qntx git credential).
const githubCredentialPath = githubPath + "/credential"

// gitCredentialUser is the username GitHub takes an installation token under
// over HTTPS.
const gitCredentialUser = "x-access-token"

// HandleGitHubCredential mints a short-lived token of the App's installation
// where one repository is, narrowed to it, and says what it may do with
// contents: one that only reads them is refused by GitHub on a push.
func (s *QNTXServer) HandleGitHubCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, githubCredentialPath+" answers POST, and this was "+r.Method)
		return
	}
	var asked struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
		writeError(w, http.StatusBadRequest, "what was sent is not a JSON object naming owner and repo: "+err.Error())
		return
	}
	if asked.Owner == "" || asked.Repo == "" {
		writeError(w, http.StatusBadRequest, "a credential is minted for one repository, and this named owner "+
			`"`+asked.Owner+`" and repo "`+asked.Repo+`"`)
		return
	}

	minted, err := s.gitHubService().InstallationToken(r.Context(), asked.Owner, asked.Repo)
	var notInstalled services.NoInstallation
	switch {
	case err == nil:
	case errors.As(err, &notInstalled):
		// GitHub answers 404 both where the App is not installed and where
		// there is no such repository: its words are given as they came.
		writeError(w, http.StatusNotFound, err.Error())
		return
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"username":   gitCredentialUser,
		"password":   minted.Token,
		"expires_at": minted.ExpiresAt,
		"app":        minted.App,
		"contents":   minted.Contents,
	})
}
