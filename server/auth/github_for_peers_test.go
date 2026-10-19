package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors "github.com/teranos/sacred-error"
)

// primaryAskingGitHub is a primary serving its routes, asking GitHub through
// the fake given, with a peer declared; and that peer.
func primaryAskingGitHub(t *testing.T, github GitHubGet, locate GitHubLocate) (primary *Handler, peer *Handler) {
	t.Helper()
	primary, _, _ = authorizingHandler(t)
	key, did := otherNode(t)
	require.NoError(t, primary.DeclarePeer(peerName, did, []string{"Ground"}, elsewhereOrigin, elsewhereOrigin))
	primary.SetGitHubForPeers(github, locate)
	mux := http.NewServeMux()
	for path, handle := range primary.Routes() {
		mux.HandleFunc(path, handle)
	}
	served := httptest.NewServer(mux)
	t.Cleanup(served.Close)
	primary.configuredOrigin = served.URL

	peer = handlerWithDoors(t)
	peer.nodeKey = key
	require.NoError(t, peer.SetPrimary(served.URL))
	return primary, peer
}

// "can k make use of the github integration setup on q ?"
func TestAPeerAsksGitHubThroughItsPrimary(t *testing.T) {
	var asked []string
	_, peer := primaryAskingGitHub(t, func(_ context.Context, url string) ([]byte, error) {
		asked = append(asked, url)
		return []byte(`{"workflow_runs":[]}`), nil
	}, locatesGitHubForNobody)
	get, _, throughThePrimary := peer.GitHubThroughThePrimary()
	require.True(t, throughThePrimary)

	body, err := get(context.Background(), "https://api.github.com/repos/teranos/ground/actions/runs?per_page=5")
	require.NoError(t, err)
	assert.JSONEq(t, `{"workflow_runs":[]}`, string(body))
	assert.Equal(t, []string{"https://api.github.com/repos/teranos/ground/actions/runs?per_page=5"}, asked)
}

// "cant k use piggyback on what q offers"
// The primary answers where GitHub points, and the peer fetches it there.
func TestAPeerAsksThePrimaryWhereGitHubPoints(t *testing.T) {
	var asked []string
	_, peer := primaryAskingGitHub(t, asksGitHubForNobody, func(_ context.Context, url string) (string, error) {
		asked = append(asked, url)
		return "https://codeload.github.com/teranos/ground/legacy.tar.gz/abc?token=signed", nil
	})
	_, locate, throughThePrimary := peer.GitHubThroughThePrimary()
	require.True(t, throughThePrimary)

	location, err := locate(context.Background(), "https://api.github.com/repos/teranos/ground/tarball/abc")
	require.NoError(t, err)
	assert.Equal(t, "https://codeload.github.com/teranos/ground/legacy.tar.gz/abc?token=signed", location)
	assert.Equal(t, []string{"https://api.github.com/repos/teranos/ground/tarball/abc"}, asked)

	_, err = locate(context.Background(), "https://example.com/anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// GitHub's refusal, or the primary's, reaches the peer in words rather than as a body.
func TestWhatGitHubRefusedReachesThePeer(t *testing.T) {
	_, peer := primaryAskingGitHub(t, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("github's rate limit is spent until 2026-10-12T16:00:00Z")
	}, locatesGitHubForNobody)
	get, _, _ := peer.GitHubThroughThePrimary()
	_, err := get(context.Background(), "https://api.github.com/repos/teranos/ground/actions/runs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit is spent")
}

// The primary asks GitHub's API for a peer and nothing else.
func TestThePrimaryAsksOnlyGitHubsAPIForAPeer(t *testing.T) {
	_, peer := primaryAskingGitHub(t, func(context.Context, string) ([]byte, error) {
		t.Fatal("a URL outside GitHub's API was asked")
		return nil, nil
	}, locatesGitHubForNobody)
	get, _, _ := peer.GitHubThroughThePrimary()
	_, err := get(context.Background(), "https://example.com/anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// A stranger asking the route, carrying no node's assertion, is refused.
func TestOnlyANodeAsksGitHubOfThePrimary(t *testing.T) {
	primary, _ := primaryAskingGitHub(t, func(context.Context, string) ([]byte, error) {
		t.Fatal("GitHub was asked for a stranger")
		return nil, nil
	}, locatesGitHubForNobody)
	form := url.Values{"url": {"https://api.github.com/repos/x/y"}}
	r := httptest.NewRequest(http.MethodPost, githubForPeersPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	primary.handleGitHubForPeers(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// A node with no primary asks GitHub itself.
func TestANodeWithNoPrimaryAsksGitHubItself(t *testing.T) {
	_, _, throughThePrimary := handlerWithDoors(t).GitHubThroughThePrimary()
	assert.False(t, throughThePrimary)
}
