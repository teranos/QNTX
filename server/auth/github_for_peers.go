package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "can k make use of the github integration setup on q ?"
// The primary holds the token for github.com. A peer's ci.watch asks GitHub
// through it, signing as its did:key, and the token never leaves the primary.

// githubForPeersPath is where a peer asks its primary one GET of GitHub's API.
const githubForPeersPath = "/auth/github"

// TODO(forge): Asking the primary node to ask GitHub for a peer is for any forge: the primary reaches the repository's host.
// peersGitHubAPI is the one host the primary asks on a peer's behalf.
const peersGitHubAPI = "https://api.github.com/"

// GitHubGet is one GET of GitHub's API: the body whole, or GitHub's own
// refusal in its words.
type GitHubGet func(ctx context.Context, url string) ([]byte, error)

// SetGitHubForPeers hands the primary how it asks GitHub, for the peers that
// ask through it.
func (h *Handler) SetGitHubForPeers(get GitHubGet) {
	h.githubForPeers = get
}

// handleGitHubForPeers answers a peer, signing as its did:key, with what
// GitHub said to the URL it named, and refuses a URL that is not GitHub's API.
func (h *Handler) handleGitHubForPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return
	}
	if r.PostForm.Get("client_assertion_type") != clientAssertionType {
		h.writeError(w, http.StatusUnauthorized, "a node asks by signing as its did:key, and this carried no client assertion")
		return
	}
	asked, err := h.assertedClient(r.PostForm, time.Now())
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "the asking node did not prove itself")
		h.logger.Infow("GitHub ask refused", "client", r.PostFormValue("client_id"), "error", err)
		return
	}
	asker, ok := h.clientByDID(asked.GetID())
	if !ok || !asker.Node {
		h.writeError(w, http.StatusUnauthorized, "only a node asks GitHub through this one")
		return
	}
	target := r.PostForm.Get("url")
	if !strings.HasPrefix(target, peersGitHubAPI) {
		h.writeError(w, http.StatusBadRequest, "this node asks "+peersGitHubAPI+" for a peer and nothing else")
		return
	}
	body, err := h.githubForPeers(r.Context(), target)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// GitHub's answer as it came: JSON, which is what githubGet hands back.
	h.writeJSON(w, http.StatusOK, json.RawMessage(body))
}

// GitHubThroughThePrimary is how a peer asks GitHub, or false on a node with
// no primary, which asks with a token of its own.
func (h *Handler) GitHubThroughThePrimary() (GitHubGet, bool) {
	if !h.logsInElsewhere {
		return nil, false
	}
	return h.askGitHubOfThePrimary, true
}

// askGitHubOfThePrimary asks the primary for one GET of GitHub's API, signing as this node.
func (h *Handler) askGitHubOfThePrimary(ctx context.Context, target string) ([]byte, error) {
	did, signed, err := h.asserted(h.loginAt, time.Now())
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	form.Set("url", target)
	at := h.loginAt + githubForPeersPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, at, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.Wrapf(err, "%s is not a request the primary answers", at)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not answer", at)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return nil, errors.Wrap(closeErr, "the primary's answer for github did not close")
	}
	if readErr != nil {
		return nil, errors.Wrap(readErr, "the primary's answer for github did not read")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("%s answered %d for %s: %s", at, resp.StatusCode, target, body)
	}
	return body, nil
}

// asksGitHubForNobody is a node handed no way to GitHub: it answers a peer so.
func asksGitHubForNobody(context.Context, string) ([]byte, error) {
	return nil, errors.New("this node holds no way to ask GitHub for a peer")
}
