package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
)

// "QNTX has a lot of github related functionality that we arent properly utilizing"
// What GitHubService knows of GitHub is asked through a sigil, as the App's
// installation where the repository is (ADR-048).
func TestGitHubIsAskedThroughASigilAsTheInstallation(t *testing.T) {
	s, asked := appHoldingServer(t, "write")

	got, refused := answer(t, s, "ask", sigil.Sent{
		"operation": "ListPullRequests",
		"request":   `{"owner": "teranos", "repo": "QNTX", "state": "open"}`,
	})
	// The stand-in knows no pulls route, and what GitHub said comes back as the refusal.
	require.NotNil(t, refused)
	assert.Nil(t, got)
	assert.Contains(t, refused.GetSays(), "GET /repos/teranos/QNTX/pulls")
	assert.Contains(t, refused.GetSays(), "Not Found")

	require.Len(t, *asked, 3)
	assert.Equal(t, "GET /repos/teranos/QNTX/installation", (*asked)[0].Route)
	assert.Equal(t, "POST /app/installations/7/access_tokens", (*asked)[1].Route)
	assert.Equal(t, "GET /repos/teranos/QNTX/pulls", (*asked)[2].Route)
}

// What it answers is GitHub's answer, as the operation gives it.
func TestWhatGitHubAnswersIsGivenAsItIs(t *testing.T) {
	s, _ := appHoldingServer(t, "write")
	got, refused := answer(t, s, "ask", sigil.Sent{
		"operation": "GetARepositoryInstallationForTheAuthenticatedApp",
		"request":   `{"owner": "teranos", "repo": "QNTX"}`,
	})
	require.Nil(t, refused)
	given, ok := got.(map[string]any)
	require.True(t, ok)
	answered, ok := given["answer"].(map[string]any)
	require.True(t, ok, "answer is %T", given["answer"])
	assert.Equal(t, "the-app", answered["app_slug"])
	holds(t, s.githubSignum(), "ask", got)
}

// An operation GitHubService does not have, what an operation does not take,
// and a request that is not JSON are each said by the param to change.
func TestGitHubAskSaysWhatToChange(t *testing.T) {
	s, asked := appHoldingServer(t, "write")
	for name, c := range map[string]struct {
		sent  sigil.Sent
		param string
		says  string
	}{
		"no such operation": {sigil.Sent{"operation": "OpenAPullRequest"}, "operation", "OpenAPullRequest"},
		"a field it does not take": {sigil.Sent{"operation": "CreateAPullRequest", "request": `{"owner": "teranos", "repo": "QNTX", "tittle": "x"}`},
			"request", "tittle"},
		"not JSON": {sigil.Sent{"operation": "CreateAPullRequest", "request": `owner=teranos`}, "request", "CreateAPullRequest"},
	} {
		_, refused := answer(t, s, "ask", c.sent)
		require.NotNil(t, refused, name)
		assert.Equal(t, c.param, refused.GetParam(), name)
		assert.Contains(t, refused.GetSays(), c.says, name)
	}
	assert.Empty(t, *asked, "GitHub was asked something that was refused here")
}

// What can be asked is listed, so whoever asks does not have to guess a name.
func TestGitHubListsItsOperations(t *testing.T) {
	s, _ := appHoldingServer(t, "write")
	got, refused := answer(t, s, "operations", sigil.Sent{})
	require.Nil(t, refused)
	listed, ok := got.(map[string]any)["operations"].([]services.GitHubOperation)
	require.True(t, ok)
	var names []string
	for _, op := range listed {
		names = append(names, op.Name)
	}
	assert.Contains(t, names, "CreateAPullRequest")
	assert.Contains(t, names, "ListWorkflowRunsForARepository")
	holds(t, s.githubSignum(), "operations", got)
}
