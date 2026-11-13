package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/apns"
)

// "AND SO IS THE APPROCAL"
// An approval stands on the phone as a Live Activity from when every check on
// its head is done, started once: the pull request, its checks, and the yes
// and the no as the node's own answer takes them. Answered, it ends saying so.
func TestAnApprovalThatWaitsOnROOTStandsAsALiveActivity(t *testing.T) {
	s, _ := approvingServer(t)
	apple := &told{}
	s.shows = apple
	code, said := handsOver(t, s, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	// Its checks are not done: it cannot be decided, and it does not stand.
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	assert.Empty(t, apple.sent, "an approval stood on the phone before it could be decided")

	// Every check passed: it stands, once, however often the node looks.
	checksPass(t, s, "abe92a9")
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 1, "the approval was not started once")
	one := apple.sent[0]
	assert.Equal(t, aPhone, one.Token)
	assert.Equal(t, apns.ActivityStarts, one.Event)
	assert.Equal(t, waitsType, one.Type)
	assert.Equal(t, waitsOf{Waits: "approval/teranos/elements#29", Kind: "approvals", Title: "teranos/elements#29"}, one.Attributes)
	shown, isOne := one.State.(waitsShown)
	require.True(t, isOne, "what it shows is not the one shape: %T", one.State)
	assert.Equal(t, "waits on you", shown.Says)
	assert.Equal(t, []waitsSegment{{Name: "TypeScript", State: checkDone}, {Name: "Browser", State: checkDone}}, shown.Segments)
	assert.Zero(t, shown.Due, "an approval counts down to nothing")
	require.Len(t, shown.Presses, 2)
	yes, no := shown.Presses[0], shown.Presses[1]
	assert.Equal(t, "approve", yes.Does)
	assert.Equal(t, "cancel", no.Does)
	assert.Equal(t, approvalsAnswerPath, yes.Path)
	assert.Equal(t, map[string]string{"subject": "teranos/elements#29", "sha": "abe92a9", "option": optionMerge, "said": saysMergeWhenReady}, yes.Sent)

	// The app hands over its own token, and ROOT presses the no on the island:
	// what the press sends is what the node's answer takes.
	code, said = handsOver(t, s, inROOTsSession(), "{\"token\":\""+anActivity+"\",\"platform\":\"ios\",\"waits\":\"approval/teranos/elements#29\"}")
	require.Equal(t, http.StatusOK, code, said)
	status, answered := press(s, inROOTsSession(), no.Sent["subject"], no.Sent["sha"], no.Sent["option"], no.Sent["said"])
	require.Equal(t, http.StatusOK, status, answered)

	// Answered: it ends saying what ROOT said, and nothing is started for that head again.
	at := time.Now()
	require.NoError(t, s.showWhatWaits(context.Background(), at))
	last := apple.sent[len(apple.sent)-1]
	assert.Equal(t, anActivity, last.Token)
	assert.Equal(t, apns.ActivityEnds, last.Event)
	assert.Equal(t, saysCancel, last.State.(waitsShown).Says)
	count := len(apple.sent)
	require.NoError(t, s.showWhatWaits(context.Background(), at.Add(time.Minute)))
	assert.Len(t, apple.sent, count, "something was said to an approval that is over")
}
