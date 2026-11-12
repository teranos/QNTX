package roomcomments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] What an agent was told is kept per room and per agent, and read back.」
func TestWhatAnAgentWasToldIsKept(t *testing.T) {
	told := Told{DB: qntxtest.CreateTestDB(t)}

	at, err := told.Mark(t.Context(), "room-1", "claude")
	require.NoError(t, err)
	assert.Empty(t, at, "never told")

	require.NoError(t, told.Keep(t.Context(), "room-1", "claude", "2026-11-09T20:00:00.173000Z"))
	require.NoError(t, told.Keep(t.Context(), "room-1", "claude", "2026-11-09T21:00:00.000000Z"))
	require.NoError(t, told.Keep(t.Context(), "room-1", "codex", "2026-11-09T19:00:00.000000Z"))

	at, err = told.Mark(t.Context(), "room-1", "claude")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-09T21:00:00.000000Z", at, "the last keeping is the mark")
	at, err = told.Mark(t.Context(), "room-1", "codex")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-09T19:00:00.000000Z", at, "another agent has its own")
	at, err = told.Mark(t.Context(), "room-2", "claude")
	require.NoError(t, err)
	assert.Empty(t, at, "another room has its own")
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] A room reads at or after a time, so what was told is held out by reading」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] from the next millisecond, in the form q's Node Room was read in.」
func TestWhatWasToldIsNotToldTwice(t *testing.T) {
	from, err := ReadFrom("2026-11-07T20:11:05.173000Z")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-07T20:11:05.174000Z", from)

	from, err = ReadFrom("2026-11-07T20:11:05.999000Z")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-07T20:11:06.000000Z", from, "past the second")

	_, err = ReadFrom("yesterday")
	require.Error(t, err)
}

func TestWhatTheTurnIsToldOfTheRoom(t *testing.T) {
	said := []Said{{At: "2026-11-09T20:00:00Z", Sender: "qodo-code-review", Body: "Server tests fail.\n"}, {At: "2026-11-09T20:05:00Z", Sender: "someone", Body: "Agreed."}}
	assert.Equal(t, "What was said in the room since you were last told:\n\n[2026-11-09T20:00:00Z] qodo-code-review:\nServer tests fail.\n\n[2026-11-09T20:05:00Z] someone:\nAgreed.\n", Digest(said, false))
	assert.Contains(t, Digest(said, true), "(Older than this was not read.)")
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] The Room shows a comment as said by whoever wrote it, not by the node.」
func TestWhoTheRoomShowsAsSayingIt(t *testing.T) {
	assert.Equal(t, "qodo-code-review", Comment{App: "qodo-code-review", Author: "qodo-code-review[bot]"}.Poster(), "an app by its slug")
	assert.Equal(t, "teranos", Comment{Author: "teranos"}.Poster(), "a person by their login")
	assert.Equal(t, "some-bot", Comment{Author: "some-bot[bot]"}.Poster(), "a bot with no app by its login without the suffix")
}
