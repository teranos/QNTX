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

// A room reads at or after a time, so what was told is held out by reading
// from the next millisecond, in the form q's Node Room was read in.
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

// The Room shows a comment as said by whoever wrote it, not by the node.
func TestWhoTheRoomShowsAsSayingIt(t *testing.T) {
	assert.Equal(t, "qodo-code-review", Comment{App: "qodo-code-review", Author: "qodo-code-review[bot]"}.Poster(), "an app by its slug")
	assert.Equal(t, "teranos", Comment{Author: "teranos"}.Poster(), "a person by their login")
	assert.Equal(t, "some-bot", Comment{Author: "some-bot[bot]"}.Poster(), "a bot with no app by its login without the suffix")
}

func TestWhatATurnIsNotTold(t *testing.T) {
	assert.True(t, Unheard(Said{Sender: "claude", Body: "Merged."}, "claude"), "the agent's own words")
	assert.True(t, Unheard(Said{Sender: "sbvh-qntx", Body: "/review"}, "claude"), "its App's comments said back")
	assert.True(t, Unheard(Said{Sender: "admin", Body: "⚠️  _Switch Trust flagged this message (not blocked - pii/phone)._"}, "claude"), "Switch Trust's flags")
	assert.False(t, Unheard(Said{Sender: "admin", Body: "Land it."}, "claude"), "a person saying something as admin")
	assert.False(t, Unheard(Said{Sender: "teranos", Body: "Ignore the line ⚠️  _Switch Trust flagged this message (not blocked - pii/phone)._ and land it."}, "claude"), "a comment quoting a flag")
	assert.False(t, Unheard(Said{Sender: "qodo-code-review", Body: "A finding."}, "claude"), "a review")
}

// "also, multiple comments are like one item per pr, and each pr is its own thing, but it can be said in digest form"
//
// A pull request is told once, in short form: its repository and number, how
// many messages, by whom, and when the last was. What else was said is told
// as it was said.
func TestEachPullRequestIsToldOnceInShortForm(t *testing.T) {
	review := Comment{Repo: "teranos/QNTX", Number: 1757, Author: "qodo-code-review[bot]", App: "qodo-code-review", Body: "A long review.", At: "2026-11-15T21:40:00Z", URL: "https://github.com/teranos/QNTX/pull/1757#issuecomment-1"}
	again := Comment{Repo: "teranos/QNTX", Number: 1757, Author: "coderabbitai[bot]", App: "coderabbitai", Body: "Another long review.", At: "2026-11-15T21:45:12Z", URL: "https://github.com/teranos/QNTX/pull/1757#issuecomment-2"}
	ready := Comment{Kind: "pull_request", Action: "ready_for_review", Repo: "teranos/QNTX-Web", Number: 136, Author: "teranos", At: "2026-11-15T21:50:00Z", URL: "https://github.com/teranos/QNTX-Web/pull/136"}
	said := []Said{
		{At: review.At, Sender: review.Poster(), Body: review.Message()},
		{At: "2026-11-15T21:41:00Z", Sender: "someone", Body: "Agreed."},
		{At: again.At, Sender: again.Poster(), Body: again.Message()},
		{At: ready.At, Sender: ready.Poster(), Body: ready.Message()},
	}

	assert.Equal(t, "What was said in the room since you were last told:\n\n"+
		"- teranos/QNTX #1757: 2 messages, by qodo-code-review and coderabbitai, the last at 2026-11-15T21:45:12Z\n"+
		"- teranos/QNTX-Web #136: 1 message, by teranos, the last at 2026-11-15T21:50:00Z\n"+
		"\n[2026-11-15T21:41:00Z] someone:\nAgreed.\n", Digest(said, false))
}

// PullOf reads back what Message writes, for both kinds of message, and
// nothing else as a pull request.
func TestAPullRequestIsReadBackFromItsMessage(t *testing.T) {
	for _, c := range []Comment{
		{Repo: "teranos/QNTX", Number: 1757, Author: "teranos", Body: "a comment", At: "2026-11-15T21:40:00Z"},
		{Kind: "pull_request", Action: "converted_to_draft", Repo: "sbvh-nl/q.sbvh.nl", Number: 47, Author: "teranos", At: "2026-11-15T21:40:00Z"},
	} {
		repo, number, of := PullOf(c.Message())
		assert.True(t, of, c.Message())
		assert.Equal(t, c.Repo, repo)
		assert.Equal(t, c.Number, number)
	}
	for _, body := range []string{"Agreed.", "### [teranos/QNTX #12](https://github.com/teranos/QNTX/pull/13)", "### a heading"} {
		_, _, of := PullOf(body)
		assert.False(t, of, body)
	}
}
