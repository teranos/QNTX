package roomcomments

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "review comments need to wake only when no comment was placed 30min since the
// last comment"
func TestAWakeWaitsUntilNoCommentCameForTheQuietStretch(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	room := Target{Provider: "switchai-py", Room: "node"}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	quiet := 30 * time.Minute

	require.NoError(t, due.Defer(t.Context(), room, "claude", "github-app:qodo-code-review", "2026-11-10T09:00:00Z", start.Add(quiet)))

	ready, err := due.Ready(t.Context(), start.Add(quiet-time.Second))
	require.NoError(t, err)
	assert.Empty(t, ready, "not before the stretch is out")

	// A second comment ten minutes in moves the wake to thirty minutes after it.
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

// A comment that moves the wake while it is being taken leaves it, and a wake
// is taken once.
func TestAWakeIsTakenOnceAndNotWhenACommentMovedIt(t *testing.T) {
	due := Due{DB: qntxtest.CreateTestDB(t)}
	room := Target{Provider: "switchai-py", Room: "node"}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	require.NoError(t, due.Defer(t.Context(), room, "claude", "s", "2026-11-10T09:00:00Z", start))
	ready, err := due.Ready(t.Context(), start)
	require.NoError(t, err)
	require.Len(t, ready, 1)

	require.NoError(t, due.Defer(t.Context(), room, "claude", "s", "2026-11-10T09:05:00Z", start.Add(time.Hour)))
	took, err := due.Take(t.Context(), ready[0])
	require.NoError(t, err)
	assert.False(t, took, "moved later since it was read")

	ready, err = due.Ready(t.Context(), start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, ready, 1)
	took, err = due.Take(t.Context(), ready[0])
	require.NoError(t, err)
	assert.True(t, took)
	took, err = due.Take(t.Context(), ready[0])
	require.NoError(t, err)
	assert.False(t, took, "once")
}

// A review's comments wait for a quiet room; a comment on the pull request
// itself wakes at once.
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
