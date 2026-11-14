package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/internal/apns"
)

// "「[Opus 5.5] I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT」"
// 「[Opus 5.5] An approval is not started as a Live Activity of its own, asked or」
// 「[Opus 5.5] decidable: it is an item in the collection of whoever it is going to.」
func TestAnApprovalIsNotStartedOnItsOwn(t *testing.T) {
	s, _ := approvingServer(t)
	apple := &told{}
	s.shows = apple
	code, said := handsOver(t, s, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	checksPass(t, s, "abe92a9")
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	assert.Empty(t, apple.sent, "an approval was started on its own")
}

// 「[Opus 5.5] One that stands from before is moved as its checks run, has the yes and the」
// 「[Opus 5.5] no once it can be decided, as the node's own answer takes them, and ends」
// 「[Opus 5.5] saying what ROOT answered.」
func TestAnApprovalThatStandsIsMovedAndEnded(t *testing.T) {
	s, _ := approvingServer(t)
	apple := &told{}
	s.shows = apple
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	code, said := handsOver(t, s, inROOTsSession(), "{\"token\":\""+anActivity+"\",\"platform\":\"ios\",\"waits\":\"approval/teranos/elements#29\"}")
	require.Equal(t, http.StatusOK, code, said)

	// 「[Opus 5.5] A check runs: it shows it, and has nothing to press, for it cannot be decided yet.」
	delivered(t, s, "check_run", checkRun("TypeScript", "abe92a9", "in_progress", ""))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 1)
	runs := apple.sent[0]
	assert.Equal(t, anActivity, runs.Token)
	assert.Equal(t, apns.ActivityMoves, runs.Event)
	assert.Equal(t, "its checks run", runs.State.(waitsShown).Says)
	assert.Equal(t, []waitsSegment{{Name: "TypeScript", State: checkRunning}}, runs.State.(waitsShown).Segments)
	assert.Empty(t, runs.State.(waitsShown).Presses, "it could be pressed before it could be decided")

	// Every check passed: it waits on ROOT, with the yes and the no.
	checksPass(t, s, "abe92a9")
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.NoError(t, s.showWhatWaits(context.Background(), time.Now()))
	require.Len(t, apple.sent, 2, "it was not moved once when its checks were done")
	shown := apple.sent[1].State.(waitsShown)
	assert.Equal(t, "waits on you", shown.Says)
	assert.Equal(t, []waitsSegment{{Name: "TypeScript", State: checkDone}, {Name: "Browser", State: checkDone}}, shown.Segments)
	require.Len(t, shown.Presses, 2)
	yes, no := shown.Presses[0], shown.Presses[1]
	assert.Equal(t, "approve", yes.Does)
	assert.Equal(t, "cancel", no.Does)
	assert.Equal(t, approvalsAnswerPath, yes.Path)
	assert.Equal(t, map[string]string{"subject": "teranos/elements#29", "sha": "abe92a9", "option": optionMerge, "said": saysMergeWhenReady}, yes.Sent)

	// 「[Opus 5.5] ROOT presses the no: what the press sends is what the node's answer takes.」
	status, answered := press(s, inROOTsSession(), no.Sent["subject"], no.Sent["sha"], no.Sent["option"], no.Sent["said"])
	require.Equal(t, http.StatusOK, status, answered)

	// 「[Opus 5.5] Answered: it ends saying what ROOT said, and nothing is said to it again.」
	at := time.Now()
	require.NoError(t, s.showWhatWaits(context.Background(), at))
	require.Len(t, apple.sent, 3)
	last := apple.sent[2]
	assert.Equal(t, anActivity, last.Token)
	assert.Equal(t, apns.ActivityEnds, last.Event)
	assert.Equal(t, saysCancel, last.State.(waitsShown).Says)
	require.NoError(t, s.showWhatWaits(context.Background(), at.Add(time.Minute)))
	assert.Len(t, apple.sent, 3, "something was said to an approval that is over")
}

// 「[Opus 5.5] A check said again in the state it was in moves nothing.」
func TestACheckSaidAgainMovesNoApproval(t *testing.T) {
	lines := func(at time.Time, predicate string, attributes map[string]any) *types.As {
		return &types.As{Subjects: []string{"teranos/elements#29"}, Predicates: []string{predicate}, Timestamp: at, Attributes: attributes}
	}
	asked := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	done := map[string]any{"sha": "abe92a9", "name": "TypeScript", "state": checkDone}
	folded := foldApprovals([]*types.As{
		lines(asked, watcher.ApprovalAsked, map[string]any{"sha": "abe92a9"}),
		lines(asked.Add(time.Minute), watcher.ApprovalChecked, map[string]any{"sha": "abe92a9", "name": "TypeScript", "state": checkRunning}),
		lines(asked.Add(2*time.Minute), watcher.ApprovalChecked, done),
		lines(asked.Add(5*time.Hour), watcher.ApprovalChecked, done),
	})
	assert.Equal(t, asked.Add(2*time.Minute), folded["teranos/elements#29"].movedAt, "a check said again in the same state moved the approval")
}
