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
// An approval stands on the phone as a Live Activity from when it is asked,
// started once: the pull request and its checks as they run. Once every check
// is done it has the yes and the no, as the node's own answer takes them.
// Answered, it ends saying so.
func TestAnApprovalThatWaitsOnROOTStandsAsALiveActivity(t *testing.T) {
	s, _ := approvingServer(t)
	apple := &told{}
	s.shows = apple
	code, said := handsOver(t, s, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	// Asked: it stands, once, however often the node looks.
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 1, "the approval was not started once")
	one := apple.sent[0]
	assert.Equal(t, aPhone, one.Token)
	assert.Equal(t, apns.ActivityStarts, one.Event)
	assert.Equal(t, waitsType, one.Type)
	assert.Equal(t, waitsOf{Waits: "approval/teranos/elements#29", Kind: "approvals", Title: "teranos/elements#29"}, one.Attributes)
	_, isOne := one.State.(waitsShown)
	require.True(t, isOne, "what it shows is not the one shape: %T", one.State)

	// The app hands over the started one's own token. A check runs: it is moved
	// to show it, and has nothing to press, for it cannot be decided yet.
	code, said = handsOver(t, s, inROOTsSession(), "{\"token\":\""+anActivity+"\",\"platform\":\"ios\",\"waits\":\"approval/teranos/elements#29\"}")
	require.Equal(t, http.StatusOK, code, said)
	delivered(t, s, "check_run", checkRun("TypeScript", "abe92a9", "in_progress", ""))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 2, "it was not moved when a check began, or was started beside itself")
	runs := apple.sent[1]
	assert.Equal(t, anActivity, runs.Token)
	assert.Equal(t, apns.ActivityMoves, runs.Event)
	assert.Equal(t, "its checks run", runs.State.(waitsShown).Says)
	assert.Equal(t, []waitsSegment{{Name: "TypeScript", State: checkRunning}}, runs.State.(waitsShown).Segments)
	assert.Empty(t, runs.State.(waitsShown).Presses, "it could be pressed before it could be decided")

	// Every check passed: it waits on ROOT, with the yes and the no.
	checksPass(t, s, "abe92a9")
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 3, "it was not moved once when its checks were done")
	shown := apple.sent[2].State.(waitsShown)
	assert.Equal(t, "waits on you", shown.Says)
	assert.Equal(t, []waitsSegment{{Name: "TypeScript", State: checkDone}, {Name: "Browser", State: checkDone}}, shown.Segments)
	assert.Zero(t, shown.Due, "an approval counts down to nothing")
	require.Len(t, shown.Presses, 2)
	yes, no := shown.Presses[0], shown.Presses[1]
	assert.Equal(t, "approve", yes.Does)
	assert.Equal(t, "cancel", no.Does)
	assert.Equal(t, approvalsAnswerPath, yes.Path)
	assert.Equal(t, map[string]string{"subject": "teranos/elements#29", "sha": "abe92a9", "option": optionMerge, "said": saysMergeWhenReady}, yes.Sent)

	// ROOT presses the no on the island: what the press sends is what the node's answer takes.
	status, answered := press(s, inROOTsSession(), no.Sent["subject"], no.Sent["sha"], no.Sent["option"], no.Sent["said"])
	require.Equal(t, http.StatusOK, status, answered)

	// Answered: it ends saying what ROOT said, and nothing is said or started for it again.
	at := time.Now()
	require.NoError(t, s.showWhatWaits(context.Background(), at))
	require.Len(t, apple.sent, 4)
	last := apple.sent[3]
	assert.Equal(t, anActivity, last.Token)
	assert.Equal(t, apns.ActivityEnds, last.Event)
	assert.Equal(t, saysCancel, last.State.(waitsShown).Says)
	require.NoError(t, s.showWhatWaits(context.Background(), at.Add(time.Minute)))
	assert.Len(t, apple.sent, 4, "something was said to an approval that is over")
}
