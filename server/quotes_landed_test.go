//go:build qntxquote

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// A merged pull request's landing is a job the queue takes: it says its pulse
// state, its moments and who made it, as the queue requires of every job.
func TestAMergedPullRequestIsEnqueuedForQuotes(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 1)
	require.NoError(t, err)
	var e gitHubPullRequestEvent
	e.Action, e.Number = "closed", 42
	e.PullRequest.Merged = true
	e.PullRequest.Head.Sha = "head42"
	e.PullRequest.Base.Ref = "main"
	e.Repository.FullName, e.Repository.DefaultBranch = "teranos/QNTX-Web", "main"
	require.NoError(t, srv.quoteLanded(e))
}
