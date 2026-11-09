package roomcomments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// What an agent was told is kept per room and per agent, and read back.
func TestWhatAnAgentWasToldIsKept(t *testing.T) {
	told := Told{DB: qntxtest.CreateTestDB(t)}

	at, id, err := told.Mark(t.Context(), "room-1", "claude")
	require.NoError(t, err)
	assert.Empty(t, at)
	assert.Empty(t, id, "never told")

	require.NoError(t, told.Keep(t.Context(), "room-1", "claude", "2026-11-09T20:00:00Z", "m1"))
	require.NoError(t, told.Keep(t.Context(), "room-1", "claude", "2026-11-09T21:00:00Z", "m2"))
	require.NoError(t, told.Keep(t.Context(), "room-1", "codex", "2026-11-09T19:00:00Z", "m0"))

	at, id, err = told.Mark(t.Context(), "room-1", "claude")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-09T21:00:00Z", at)
	assert.Equal(t, "m2", id, "the last keeping is the mark")

	_, id, err = told.Mark(t.Context(), "room-1", "codex")
	require.NoError(t, err)
	assert.Equal(t, "m0", id, "another agent has its own")
	_, id, err = told.Mark(t.Context(), "room-2", "claude")
	require.NoError(t, err)
	assert.Empty(t, id, "another room has its own")
}

// A room reads at or after a time, so the message the mark names comes back
// with the rest; what the agent was told is not told twice.
func TestWhatWasToldIsNotToldTwice(t *testing.T) {
	read := []Said{{ID: "m1", Sender: "qodo-code-review", Body: "one"}, {ID: "m2", Sender: "qodo-code-review", Body: "two"}, {ID: "m3", Sender: "someone", Body: "three"}}
	assert.Equal(t, read, After(read, ""), "never told")
	assert.Equal(t, read[2:], After(read, "m2"))
	assert.Empty(t, After(read, "m3"), "told it all")
	assert.Equal(t, read, After(read, "gone"), "a mark the room no longer holds tells everything it read")
}

func TestWhatTheTurnIsToldOfTheRoom(t *testing.T) {
	said := []Said{{ID: "m1", At: "2026-11-09T20:00:00Z", Sender: "qodo-code-review", Body: "Server tests fail.\n"}, {ID: "m2", At: "2026-11-09T20:05:00Z", Sender: "someone", Body: "Agreed."}}
	assert.Equal(t, "What was said in the room since you were last told:\n\n[2026-11-09T20:00:00Z] qodo-code-review:\nServer tests fail.\n\n[2026-11-09T20:05:00Z] someone:\nAgreed.\n", Digest(said, false))
	assert.Contains(t, Digest(said, true), "(Older than this was not read.)")
}

// The Room shows a comment as said by whoever wrote it, not by the node.
func TestWhoTheRoomShowsAsSayingIt(t *testing.T) {
	assert.Equal(t, "qodo-code-review", Comment{App: "qodo-code-review", Author: "qodo-code-review[bot]"}.Poster(), "an app by its slug")
	assert.Equal(t, "teranos", Comment{Author: "teranos"}.Poster(), "a person by their login")
	assert.Equal(t, "some-bot", Comment{Author: "some-bot[bot]"}.Poster(), "a bot with no app by its login without the suffix")
}
