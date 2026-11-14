package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// whatWaitsFor is what is collected for the agent whose DID this is, as the node keeps it.
func whatWaitsFor(t *testing.T, srv *QNTXServer, did string) *collectedFor {
	t.Helper()
	kept, err := srv.theCollection()
	require.NoError(t, err)
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	require.NoError(t, err)
	return held[did]
}

// The pull request an asking of GitHub is about is named by what was asked,
// or, for one the asking opened, by what GitHub answered.
func TestThePullRequestAnAskingIsAbout(t *testing.T) {
	about := func(request string, answered map[string]any) string {
		pull, _ := pullAskedAbout([]byte(request), answered)
		return pull
	}
	assert.Equal(t, "teranos/QNTX#1734", about(`{"owner":"teranos","repo":"QNTX","pull_number":1734}`, nil))
	assert.Equal(t, "teranos/QNTX#1734", about(`{"owner":"teranos","repo":"QNTX","issue_number":1734,"body":"Thanks"}`, nil))
	assert.Equal(t, "teranos/QNTX#1740", about(`{"owner":"teranos","repo":"QNTX","title":"t","head":"a","base":"main"}`,
		map[string]any{"number": float64(1740), "head": map[string]any{"ref": "a"}}))
	// An issue that was opened is no pull request, and a branch that was read is about none.
	assert.Empty(t, about(`{"owner":"teranos","repo":"QNTX","title":"t"}`, map[string]any{"number": float64(9)}))
	assert.Empty(t, about(`{"owner":"teranos","repo":"QNTX","branch":"main"}`, nil))
	assert.Empty(t, about(`not json`, nil))
}

// "IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION"
// A pull request that is merged is collected for each session that was
// involved in it, as something that wakes nobody. One that holds no name is
// told nothing, and nothing fails for it.
func TestAMergeIsCollectedForEachSessionThatWasInvolved(t *testing.T) {
	srv, _ := namedOverA2A(t)
	asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	asAnAgent(t, srv, "Hazel", "did:key:z6Mkhazel")
	pull := "teranos/elements#29"
	for _, did := range []string{"did:key:z6Mkalder", "did:key:z6Mkhazel", "did:key:z6Mknameless"} {
		require.NoError(t, srv.nodeRecords().nodeRecord(did, involvedIn+pull, did, "_", map[string]any{"operation": "CreateAPullRequest"}))
	}

	require.NoError(t, srv.pullIsOver(pull, "A panel on a phone reads at a phone's size", "https://github.com/teranos/elements/pull/29", true))

	for _, did := range []string{"did:key:z6Mkalder", "did:key:z6Mkhazel"} {
		waits := whatWaitsFor(t, srv, did)
		require.NotNil(t, waits, "nothing was collected for %s", did)
		require.Len(t, waits.Waiting, 1)
		assert.True(t, waits.Waiting[0].Quiet, "a merge woke the session that was involved in it")
		assert.Equal(t, "GitHub", waits.Waiting[0].From)
		assert.Contains(t, waits.Waiting[0].Says, pull+" was merged")
		assert.True(t, waits.due().IsZero())
	}
	assert.Nil(t, whatWaitsFor(t, srv, "did:key:z6Mknameless"), "something was collected for a session that holds no name")
}

// "NO, ANY ONE THAT WAKES. PR MERGE UPDATES DONT WAKE PER SE, THEY GET COLLECTED ON A WAKING COLLECTION"
// What wakes nobody is due at no time: alone it is not delivered and makes
// nothing stand. It goes with what wakes, at the time that is due.
func TestWhatWakesNobodyIsDueAtNoTimeAndGoesWithWhatWakes(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	birch := collectedFor{DID: "did:key:z6Mkbirch", Called: "Birch", Tenant: "Birch"}

	merged := time.Now()
	due, err := srv.collect(birch, collectedSaid{From: "GitHub", Says: "teranos/elements#29 was merged", At: merged, Quiet: true})
	require.NoError(t, err)
	assert.True(t, due.IsZero(), "what wakes nobody was due at %s", due)

	// A day on: not delivered, and nothing stood for it.
	require.NoError(t, srv.deliverDue(merged.Add(24*time.Hour)))
	require.NoError(t, srv.showWhatWaits(context.Background(), merged.Add(24*time.Hour)))
	waits := whatWaitsFor(t, srv, birch.DID)
	require.NotNil(t, waits)
	assert.Len(t, waits.Waiting, 1, "what wakes nobody was delivered for itself")
	assert.Empty(t, apple.sent, "what wakes nobody stood on a phone")

	// An agent says something to Birch: that wakes, and is due its wait after it was said.
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request two is open")
	waits = whatWaitsFor(t, srv, birch.DID)
	require.Len(t, waits.Waiting, 2, "what wakes nobody did not stay to go with what wakes")
	assert.WithinDuration(t, time.Now().Add(6*time.Hour), waits.due(), time.Minute, "it is due by what wakes nobody, and not by what wakes")

	// It stands now, and shows both.
	require.NoError(t, srv.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 1)
	assert.Equal(t, "2 wait", apple.sent[0].State.(waitsShown).Says)
}
