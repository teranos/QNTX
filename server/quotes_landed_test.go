//go:build qntxquote

package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/quote"
)

// A push to the default branch is a job the queue takes: it says its pulse
// state, its moments and who made it, as the queue requires of every job.
func TestAMergedPullRequestIsEnqueuedForQuotes(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 1)
	require.NoError(t, err)
	push := []byte(`{"ref":"refs/heads/main","before":"base42","after":"head42","repository":{"full_name":"teranos/QNTX-Web","default_branch":"main"},"commits":[{"message":"x\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"}]}`)
	require.NoError(t, srv.quoteLanded(push))

	// Enqueued is not taken: the queue hands out only the jobs it holds as queued.
	job, err := srv.daemon.GetQueue().Dequeue()
	require.NoError(t, err, "a landing's job was enqueued and the queue has none to hand out")
	require.Equal(t, quote.LandedHandlerName, job.HandlerName)
	require.Equal(t, async.JobStatusRunning, job.Status)
}
