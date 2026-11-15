package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/apns"
	"github.com/teranos/QNTX/server/sigil"
)

// "The failure is also quiet by construction."
// "this is more problematic than you are probably giving it credit for"
//
// What the node could not deliver is not left to read as waiting: each try is
// kept with what waits, said each time, listed with it, and it stands in front
// of ROOT on their phone, saying so and why, with its two answers.
func TestWhatCouldNotBeDeliveredSaysSoEachTimeAndStandsInFront(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	// Served nowhere on this node: every delivery to it fails.
	ghost := collectedFor{DID: "did:key:z6Mkghost", Called: "Ghost", Tenant: "Ghost"}
	at := time.Now().Add(-7 * time.Hour)
	_, err := srv.collect(ghost, collectedSaid{From: "Alder", Says: "the build is red", At: at})
	require.NoError(t, err)

	first := time.Now()
	err = srv.deliverDue(first)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ghost, try 1 since")

	// The second try says itself as the second: the text is not the same, and
	// nothing waits for the text to change to say it.
	err = srv.deliverDue(first.Add(time.Minute))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ghost, try 2 since")

	waits := whatWaitsFor(t, srv, ghost.DID)
	require.NotNil(t, waits)
	assert.Equal(t, 2, waits.Tries)
	assert.WithinDuration(t, first, waits.Failing, time.Second, "it began to fail at the first try, and that stays")
	assert.NotEmpty(t, waits.Failure)

	// Listed with what waits, so the page says it too.
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)
	assert.Equal(t, int32(2), all.GetCollected()[0].GetTries())
	assert.NotEmpty(t, all.GetCollected()[0].GetFailingSince())
	assert.Equal(t, waits.Failure, all.GetCollected()[0].GetFailure())

	// Past when it was due, and standing all the same: it waits on ROOT now.
	require.NoError(t, srv.showWhatWaits(context.Background(), first.Add(time.Minute)))
	require.Len(t, apple.sent, 1, "what could not be delivered did not stand on the phone")
	shown := apple.sent[0]
	assert.Equal(t, apns.ActivityStarts, shown.Event)
	state := shown.State.(waitsShown)
	assert.Equal(t, "not delivered, 2 tries", state.Says)
	assert.Equal(t, waits.Failure[:min(len(waits.Failure), 40)], state.Preview[:min(len(state.Preview), 40)])
	require.Len(t, state.Presses, 2)
	assert.Equal(t, "approve", state.Presses[0].Does)
	assert.Equal(t, "cancel", state.Presses[1].Does)
	require.Len(t, state.Segments, 1)
	assert.Equal(t, "failed", state.Segments[0].State)
	// It stands one wait from when it began to fail, and not past it.
	assert.WithinDuration(t, waits.Failing.Add(waitOf(waits.Model)), shown.Expires, time.Second)

	// Looked at again, it is not started twice.
	require.NoError(t, srv.showWhatWaits(context.Background(), first.Add(2*time.Minute)))
	assert.Len(t, apple.sent, 1)

	// One wait after it began to fail it stands no longer: no Live Activity
	// stands past Apple's eight hours.
	assert.False(t, waits.stands(waits.Failing.Add(waitOf(waits.Model))))
}

// Delivered, what waits is no longer failing: a try that worked clears the
// tries that did not.
func TestADeliveryThatWorkedClearsTheTriesThatDidNot(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	kept, err := srv.theCollection()
	require.NoError(t, err)
	_, _, err = srv.failedToDeliver(kept, "did:key:z6Mkbirch", assert.AnError, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, whatWaitsFor(t, srv, "did:key:z6Mkbirch").Tries)

	_, refused := askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	waits := whatWaitsFor(t, srv, "did:key:z6Mkbirch")
	require.NotNil(t, waits)
	assert.Zero(t, waits.Tries)
	assert.True(t, waits.Failing.IsZero())
	assert.Empty(t, waits.Failure)
}
