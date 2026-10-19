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

// "cant k use piggyback on what q offers"
// A peer's comet asks the same way: the commit a branch is at, and where
// GitHub keeps the archive of it.

// githubForPeersPath is where a peer asks its primary one GET of GitHub's API.
const githubForPeersPath = "/auth/github"

// githubLocationForPeersPath is where a peer asks its primary where GitHub
// points one GET of its API: an archive's signed URL, which the peer fetches.
const githubLocationForPeersPath = "/auth/github/location"

// TODO(forge): Asking the primary node to ask GitHub for a peer is for any forge: the primary reaches the repository's host.
// peersGitHubAPI is the one host the primary asks on a peer's behalf.
const peersGitHubAPI = "https://api.github.com/"

// GitHubGet is one GET of GitHub's API: the body whole, or GitHub's own
// refusal in its words.
type GitHubGet func(ctx context.Context, url string) ([]byte, error)

// GitHubLocate is one GET of GitHub's API whose answer is where it points:
// the Location of its redirect, or GitHub's own refusal in its words.
type GitHubLocate func(ctx context.Context, url string) (string, error)

// githubLocation is what the primary answers a peer asking where GitHub points.
type githubLocation struct {
	Location string `json:"location"`
}

// SetGitHubForPeers hands the primary how it asks GitHub, for the peers that
// ask through it.
func (h *Handler) SetGitHubForPeers(get GitHubGet, locate GitHubLocate) {
	h.githubForPeers = get
	h.githubLocationForPeers = locate
}

// handleGitHubForPeers answers a peer, signing as its did:key, with what
// GitHub said to the URL it named, and refuses a URL that is not GitHub's API.
func (h *Handler) handleGitHubForPeers(w http.ResponseWriter, r *http.Request) {
	target, proven := h.peerAskingGitHub(w, r)
	if !proven {
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

// handleGitHubLocationForPeers answers a peer, signing as its did:key, with
// where GitHub points the URL it named.
func (h *Handler) handleGitHubLocationForPeers(w http.ResponseWriter, r *http.Request) {
	target, proven := h.peerAskingGitHub(w, r)
	if !proven {
		return
	}
	location, err := h.githubLocationForPeers(r.Context(), target)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, githubLocation{Location: location})
}

// peerAskingGitHub is the URL of GitHub's API a peer, signing as its did:key,
// asks for; or the refusal written, and false.
func (h *Handler) peerAskingGitHub(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return "", false
	}
	if r.PostForm.Get("client_assertion_type") != clientAssertionType {
		h.writeError(w, http.StatusUnauthorized, "a node asks by signing as its did:key, and this carried no client assertion")
		return "", false
	}
	asked, err := h.assertedClient(r.PostForm, time.Now())
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "the asking node did not prove itself")
		h.logger.Infow("GitHub ask refused", "client", r.PostFormValue("client_id"), "path", r.URL.Path, "error", err)
		return "", false
	}
	asker, ok := h.clientByDID(asked.GetID())
	if !ok || !asker.Node {
		h.writeError(w, http.StatusUnauthorized, "only a node asks GitHub through this one")
		return "", false
	}
	target := r.PostForm.Get("url")
	if !strings.HasPrefix(target, peersGitHubAPI) {
		h.writeError(w, http.StatusBadRequest, "this node asks "+peersGitHubAPI+" for a peer and nothing else")
		return "", false
	}
	return target, true
}

// GitHubThroughThePrimary is how a peer asks GitHub, or false on a node with
// no primary, which asks with a token of its own.
func (h *Handler) GitHubThroughThePrimary() (GitHubGet, GitHubLocate, bool) {
	if !h.logsInElsewhere {
		return nil, nil, false
	}
	return h.askGitHubOfThePrimary, h.locateGitHubOfThePrimary, true
}

// askGitHubOfThePrimary asks the primary for one GET of GitHub's API, signing as this node.
func (h *Handler) askGitHubOfThePrimary(ctx context.Context, target string) ([]byte, error) {
	return h.askThePrimaryOfGitHub(ctx, githubForPeersPath, target)
}

// locateGitHubOfThePrimary asks the primary where GitHub points one GET of its API, signing as this node.
func (h *Handler) locateGitHubOfThePrimary(ctx context.Context, target string) (string, error) {
	body, err := h.askThePrimaryOfGitHub(ctx, githubLocationForPeersPath, target)
	if err != nil {
		return "", err
	}
	var answered githubLocation
	if err := json.Unmarshal(body, &answered); err != nil {
		return "", errors.Wrapf(err, "the primary's answer for where GitHub points %s did not read: %s", target, body)
	}
	if answered.Location == "" {
		return "", errors.Newf("the primary answered no location for %s: %s", target, body)
	}
	return answered.Location, nil
}

// askThePrimaryOfGitHub posts target to the primary's path, signing as this node.
func (h *Handler) askThePrimaryOfGitHub(ctx context.Context, path, target string) ([]byte, error) {
	did, signed, err := h.asserted(h.loginAt, time.Now())
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	form.Set("url", target)
	at := h.loginAt + path
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
		return nil, errors.Wrapf(closeErr, "the primary's answer at %s did not close", at)
	}
	if readErr != nil {
		return nil, errors.Wrapf(readErr, "the primary's answer at %s did not read", at)
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

// locatesGitHubForNobody is a node handed no way to GitHub: it answers a peer so.
func locatesGitHubForNobody(context.Context, string) (string, error) {
	return "", errors.New("this node holds no way to ask GitHub for a peer")
}
