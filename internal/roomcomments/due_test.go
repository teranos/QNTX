package roomcomments

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "review comments need to wake only when no comment was placed 30min since the」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] last comment"」
func TestAWakeWaitsUntilNoCommentCameForTheQuietStretch(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	room := Target{Provider: "switchai-py", Room: "node"}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	quiet := 30 * time.Minute

	require.NoError(t, due.Defer(t.Context(), room, "claude", "github-app:qodo-code-review", "2026-11-10T09:00:00Z", start.Add(quiet)))

	ready, err := due.Ready(t.Context(), start.Add(quiet-time.Second))
	require.NoError(t, err)
	assert.Empty(t, ready, "not before the stretch is out")

	// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] A second comment ten minutes in moves the wake to thirty minutes after it.」
	later := start.Add(10 * time.Minute)
	require.NoError(t, due.Defer(t.Context(), room, "claude", "github-app:qodo-code-review", "2026-11-10T09:10:00Z", later.Add(quiet)))

	ready, err = due.Ready(t.Context(), start.Add(quiet+time.Minute))
	require.NoError(t, err)
	assert.Empty(t, ready, "the first comment's stretch is not the wake's now")

	ready, err = due.Ready(t.Context(), later.Add(quiet))
	require.NoError(t, err)
	require.Len(t, ready, 1)
	assert.Equal(t, room, ready[0].Target)
	assert.Equal(t, "claude", ready[0].Agent)
	assert.Equal(t, "2026-11-10T09:00:00Z", ready[0].Since, "the agent is told from the first comment of the stretch")
}

// 「[Opus 5.5] A comment that moves the wake while it is being claimed leaves it, and a」
// 「[Opus 5.5] wake is claimed once.」
func TestAWakeIsClaimedOnceAndNotWhenACommentMovedIt(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	room := Target{Provider: "switchai-py", Room: "node"}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	require.NoError(t, due.Defer(t.Context(), room, "claude", "s", "2026-11-10T09:00:00Z", start))
	ready, err := due.Ready(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, ready, 1)

	require.NoError(t, due.Defer(t.Context(), room, "claude", "s", "2026-11-10T09:05:00Z", start.Add(time.Hour)))
	_, took, err := due.Claim(t.Context(), ready[0], start.Add(30*time.Minute))
	require.NoError(t, err)
	assert.False(t, took, "moved later since it was read")

	ready, err = due.Ready(t.Context(), start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, ready, 1)
	held, took, err := due.Claim(t.Context(), ready[0], start.Add(90*time.Minute))
	require.NoError(t, err)
	assert.True(t, took)
	_, took, err = due.Claim(t.Context(), ready[0], start.Add(90*time.Minute))
	require.NoError(t, err)
	assert.False(t, took, "once")

	require.NoError(t, due.Done(t.Context(), held))
	ready, err = due.Ready(t.Context(), start.Add(24*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, ready, "told, it waits no more")
}

// 「[Opus 5.5] A wake claimed and never done, as when the node stops while the agent is」
// 「[Opus 5.5] being told or the telling fails, comes again when the hold is out.」
func TestAClaimedWakeThatWasNotDoneComesAgain(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	room := Target{Provider: "switchai-py", Room: "node"}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	require.NoError(t, due.Defer(t.Context(), room, "claude", "github-app:qodo-code-review", "2026-11-10T09:00:00Z", start))
	ready, err := due.Ready(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, ready, 1)
	held, took, err := due.Claim(t.Context(), ready[0], start.Add(30*time.Minute))
	require.NoError(t, err)
	require.True(t, took)

	ready, err = due.Ready(t.Context(), start.Add(29*time.Minute))
	require.NoError(t, err)
	assert.Empty(t, ready, "held while the agent is told")
	ready, err = due.Ready(t.Context(), start.Add(30*time.Minute))
	require.NoError(t, err)
	require.Len(t, ready, 1, "never done, it comes again")
	assert.Equal(t, "2026-11-10T09:00:00Z", ready[0].Since)

	// 「[Opus 5.5] A newer comment while it is held is the wake's speaker and time from then」
	// 「[Opus 5.5] on, and done of the older claim leaves it.」
	require.NoError(t, due.Defer(t.Context(), room, "claude", "github:teranos", "2026-11-10T09:20:00Z", start.Add(50*time.Minute)))
	require.NoError(t, due.Done(t.Context(), held))
	ready, err = due.Ready(t.Context(), start.Add(50*time.Minute))
	require.NoError(t, err)
	require.Len(t, ready, 1)
	assert.Equal(t, "github:teranos", ready[0].Speaker)
}

// 「[Opus 5.5] Two providers that call a room alike each have their wake.」
func TestTwoProvidersWithOneRoomNameWaitApart(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	for _, provider := range []string{"switchai-py", "otaripy"} {
		require.NoError(t, due.Defer(t.Context(), Target{Provider: provider, Room: "node"}, "claude", "s-"+provider, "2026-11-10T09:00:00Z", start))
	}
	ready, err := due.Ready(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, ready, 2)
	held, took, err := due.Claim(t.Context(), ready[0], start.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, took)
	require.NoError(t, due.Done(t.Context(), held))
	ready, err = due.Ready(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, ready, 1, "the other provider's wake is its own")
}

// 「[Opus 5.5] A review's comments wait for a quiet room; a comment on the pull request」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] itself wakes at once.」
func TestWhichCommentsWaitForAQuietRoom(t *testing.T) {
	for event, quiet := range map[string]bool{"pull_request_review_comment": true, "pull_request_review": true, "issue_comment": false} {
		action := "created"
		if event == "pull_request_review" {
			action = "submitted"
		}
		c, err := Parse(event, delivery(t, event, "teranos/QNTX", action, "A finding."))
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, event, c.Kind)
		assert.Equal(t, quiet, c.Quiet(), event)
	}
}

func draftDelivery(t *testing.T, action string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"action":       action,
		"repository":   map[string]any{"full_name": "teranos/QNTX"},
		"pull_request": map[string]any{"id": 55, "number": 1609, "body": "Why.\n\n🤖 " + ClaudeFooter + "(https://claude.com/claude-code)", "updated_at": "2026-11-10T09:00:00Z"},
		"sender":       map[string]any{"login": "teranos"},
	})
	require.NoError(t, err)
	return b
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "Draft->Ready change that should wake. but, Ready->Draft needs not wake, but」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] should still enter the room."」
func TestAPullRequestReadyForReviewWakesAndBackToDraftDoesNot(t *testing.T) {
	ready, err := Parse("pull_request", draftDelivery(t, "ready_for_review"))
	require.NoError(t, err)
	require.NotNil(t, ready)
	assert.True(t, ready.Wakes(nil, "Node Room"), "no list of apps: a person changed it")
	assert.True(t, ready.Quiet(), "after the same quiet stretch as a review")
	assert.Equal(t, "github:teranos", ready.Speaker())
	assert.Equal(t, "teranos", ready.Poster())
	assert.Contains(t, ready.Message(), "**teranos** marked the pull request ready for review")
	assert.Contains(t, ready.Message(), "https://github.com/teranos/QNTX/pull/1609")
	assert.Empty(t, ready.Body, "no words to attach")

	draft, err := Parse("pull_request", draftDelivery(t, "converted_to_draft"))
	require.NoError(t, err)
	require.NotNil(t, draft, "it enters the room")
	assert.False(t, draft.Wakes(Guests{Everyone: {"teranos"}}, "Node Room"), "and wakes no one")
	assert.Contains(t, draft.Message(), "**teranos** converted the pull request to a draft")
	assert.NotEqual(t, ready.Key, draft.Key)

	for _, action := range []string{"opened", "synchronize", "closed", "edited"} {
		other, err := Parse("pull_request", draftDelivery(t, action))
		require.NoError(t, err)
		assert.Nil(t, other, action)
	}
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Only a pull request Claude Code opened wakes the agent that opened it.」
func TestAReadyPullRequestThatIsNotClaudesWakesNoOne(t *testing.T) {
	var sent map[string]any
	require.NoError(t, json.Unmarshal(draftDelivery(t, "ready_for_review"), &sent))
	sent["pull_request"].(map[string]any)["body"] = "Somebody else's."
	b, err := json.Marshal(sent)
	require.NoError(t, err)
	c, err := Parse("pull_request", b)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.False(t, c.Wakes(nil, "Node Room"))
}
