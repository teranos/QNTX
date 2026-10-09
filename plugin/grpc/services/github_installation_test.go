package services

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// installedOn is a GitHub where the App is installed on teranos/QNTX and mints
// a token that lasts until expires, and answers anything else with answer.
func installedOn(expires time.Time, answer http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/teranos/QNTX/installation":
			answerJSON(200, `{"id": 7, "app_slug": "the-app"}`)(w, r)
		case "POST /app/installations/7/access_tokens":
			body, _ := json.Marshal(map[string]any{
				"token": "ghs_minted", "expires_at": expires.UTC().Format(time.RFC3339),
				"permissions": map[string]string{"contents": "write"},
			})
			answerJSON(201, string(body))(w, r)
		default:
			answer(w, r)
		}
	}
}

func routesOf(seen *[]seenRequest) []string {
	var routes []string
	for _, r := range *seen {
		routes = append(routes, r.Method+" "+r.Path)
	}
	return routes
}

// An agent the node hosts holds no token and stands in no namespace's shoes
// on GitHub: what it asks is spent as the App's installation where the
// repository is (ADR-048).
func TestACallAsTheInstallationSpendsTheTokenTheAppMints(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"},
		installedOn(time.Now().Add(time.Hour), answerJSON(201, `{"number": 12, "html_url": "https://github.com/teranos/QNTX/pull/12"}`)))

	resp, err := s.CreateAPullRequest(AsInstallation(context.Background()), &protocol.GitHubCreateAPullRequestRequest{
		Owner: "teranos", Repo: "QNTX", Title: "A change", Head: "a-branch", Base: "main",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)
	assert.Equal(t, int64(12), resp.Number)

	assert.Equal(t, []string{
		"GET /repos/teranos/QNTX/installation",
		"POST /app/installations/7/access_tokens",
		"POST /repos/teranos/QNTX/pulls",
	}, routesOf(seen))
	assert.Equal(t, "Bearer the-apps-jwt", (*seen)[0].Header.Get("Authorization"))
	assert.Equal(t, "Bearer ghs_minted", (*seen)[2].Header.Get("Authorization"))
}

// A token still good is spent again: the App is asked once for what lasts an hour.
func TestAnInstallationTokenIsMintedOnceWhileItLasts(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"},
		installedOn(time.Now().Add(time.Hour), answerJSON(200, `[]`)))
	as := AsInstallation(context.Background())

	for range 3 {
		resp, err := s.ListPullRequests(as, &protocol.GitHubListPullRequestsRequest{Owner: "teranos", Repo: "QNTX"})
		require.NoError(t, err)
		require.True(t, resp.Success, resp.Error)
	}
	assert.Equal(t, []string{
		"GET /repos/teranos/QNTX/installation",
		"POST /app/installations/7/access_tokens",
		"GET /repos/teranos/QNTX/pulls",
		"GET /repos/teranos/QNTX/pulls",
		"GET /repos/teranos/QNTX/pulls",
	}, routesOf(seen))
}

// One about to run out is not spent: GitHub would refuse it mid-call.
func TestAnInstallationTokenAboutToExpireIsMintedAgain(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"},
		installedOn(time.Now().Add(30*time.Second), answerJSON(200, `[]`)))
	as := AsInstallation(context.Background())

	for range 2 {
		resp, err := s.ListPullRequests(as, &protocol.GitHubListPullRequestsRequest{Owner: "teranos", Repo: "QNTX"})
		require.NoError(t, err)
		require.True(t, resp.Success, resp.Error)
	}
	assert.Len(t, *seen, 6, "a token with seconds left was spent again: %v", routesOf(seen))
}

// What names no repository cannot be placed under an installation, and says so
// before GitHub is asked anything.
func TestACallAsTheInstallationThatNamesNoRepositoryIsRefused(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(200, `{}`))
	resp, err := s.RateLimit(AsInstallation(context.Background()), &protocol.GitHubRateLimitRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "names no repository")
	assert.Empty(t, *seen)
}

// Where the App is not installed the call is refused in GitHub's words, and no
// namespace's token is spent in its place.
func TestACallAsTheInstallationWhereTheAppIsNotInstalledIsRefused(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(404, `{"message": "Not Found"}`))
	resp, err := s.ListPullRequests(AsInstallation(context.Background()), &protocol.GitHubListPullRequestsRequest{Namespace: "garden", Owner: "elsewhere", Repo: "thing"})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "elsewhere/thing")
	assert.Contains(t, resp.Error, "Not Found")
	assert.Equal(t, []string{"GET /repos/elsewhere/thing/installation"}, routesOf(seen))
}

// A node that cannot sign as the App is not an App that is not installed, and
// neither is a GitHub that answers anything but 404: each is said as it is.
func TestOnlyGitHubsNotFoundIsTheAppNotInstalled(t *testing.T) {
	keyless, _ := fakeGitHub(t, answerJSON(200, `{}`))
	_, err := keyless.InstallationToken(context.Background(), "teranos", "QNTX")
	require.Error(t, err)
	assert.NotErrorAs(t, err, &NoInstallation{})
	assert.Contains(t, err.Error(), "private_key")

	down, _ := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(502, `{"message": "Bad Gateway"}`))
	_, err = down.InstallationToken(context.Background(), "teranos", "QNTX")
	require.Error(t, err)
	assert.NotErrorAs(t, err, &NoInstallation{})
	assert.Contains(t, err.Error(), "502")

	absent, _ := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(404, `{"message": "Not Found"}`))
	_, err = absent.InstallationToken(context.Background(), "teranos", "QNTX")
	require.Error(t, err)
	assert.ErrorAs(t, err, &NoInstallation{})
}

// GitHubService is asked by the name of an operation and what it takes, as
// JSON, by whatever reaches it without a compiled request in hand.
func TestGitHubServiceIsAskedByOperationName(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"},
		installedOn(time.Now().Add(time.Hour), answerJSON(201, `{"number": 12, "html_url": "https://github.com/teranos/QNTX/pull/12", "draft": true}`)))

	answered, err := s.Ask(AsInstallation(context.Background()), "CreateAPullRequest",
		[]byte(`{"owner": "teranos", "repo": "QNTX", "title": "A change", "head": "a-branch", "base": "main", "draft": true}`))
	require.NoError(t, err)

	var pull struct {
		Success bool   `json:"success"`
		Number  int64  `json:"number,string"`
		HTMLURL string `json:"html_url"`
	}
	require.NoError(t, json.Unmarshal(answered, &pull), "answered %s", answered)
	assert.True(t, pull.Success)
	assert.Equal(t, int64(12), pull.Number)
	assert.Equal(t, "https://github.com/teranos/QNTX/pull/12", pull.HTMLURL)
	assert.JSONEq(t, `{"title": "A change", "head": "a-branch", "base": "main", "draft": true}`, (*seen)[2].Body)
}

// An operation it does not have, and a field the operation does not take, are
// said by name: whoever asks reads what to change.
func TestGitHubServiceSaysWhatItWasAskedThatItDoesNotHave(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(200, `{}`))

	_, err := s.Ask(AsInstallation(context.Background()), "OpenAPullRequest", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OpenAPullRequest")

	_, err = s.Ask(AsInstallation(context.Background()), "CreateAPullRequest", []byte(`{"owner": "teranos", "repo": "QNTX", "tittle": "x"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tittle")
	assert.Empty(t, *seen)
}

// On the box the agent asked for a credential as a tool, and the token went
// into its context and from there into its transcript. What answers with a
// credential is the node's own to ask: it is not asked by name, and not listed.
func TestWhatAnswersWithACredentialIsNotAskedByName(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"},
		installedOn(time.Now().Add(time.Hour), answerJSON(200, `{}`)))

	_, err := s.Ask(AsInstallation(context.Background()), "CreateAnInstallationAccessTokenForAnApp", []byte(`{"installation_id": 7}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential")
	assert.Empty(t, *seen)

	for _, op := range GitHubOperations() {
		assert.NotEqual(t, "CreateAnInstallationAccessTokenForAnApp", op.GetOperation())
	}
}

// What can be asked is listed, each with where it goes on GitHub and what it takes.
func TestGitHubServiceListsWhatCanBeAsked(t *testing.T) {
	var pulls *protocol.GitHubOperation
	for _, op := range GitHubOperations() {
		if op.GetOperation() == "CreateAPullRequest" {
			pulls = op
			break
		}
	}
	require.NotNil(t, pulls, "CreateAPullRequest is not listed")
	assert.Equal(t, http.MethodPost, pulls.Method)
	assert.Equal(t, "/repos/{owner}/{repo}/pulls", pulls.Path)
	assert.Contains(t, pulls.Takes, "title")
	assert.Contains(t, pulls.Takes, "head")
	assert.NotContains(t, pulls.Takes, "namespace", "namespace is GitHubService's own, and an installation spends none")
}
