package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// "each element is an agent as well, we default to claude"
// An element named has its own continuing session with the agent, kept under
// the agent's home by the element, beside the one session the agent continues
// when no element is named (ADR-054).
func TestAnElementIsItsOwnSessionWithTheAgent(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)

	first, refused := saying(s, sigil.Sent{"says": "what is this?", "element": "agent-element"})
	require.Nil(t, refused)
	again, refused := saying(s, sigil.Sent{"says": "and this?", "element": "agent-element"})
	require.Nil(t, refused)
	alone, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	other, refused := saying(s, sigil.Sent{"says": "what is this?", "element": "canvas-7"})
	require.Nil(t, refused)

	assert.Equal(t, first.GetSession(), again.GetSession())
	assert.Equal(t, first.GetSession(), after(ranWith(t, ran, "1", "args"), "--resume"))
	assert.NotEqual(t, first.GetSession(), alone.GetSession())
	assert.NotEqual(t, first.GetSession(), other.GetSession())
	assert.NotEqual(t, alone.GetSession(), other.GetSession())

	home := s.nodeAgent.home
	kept, err := os.ReadFile(filepath.Join(home, "elements", "agent-element", claudeSessionFile))
	require.NoError(t, err)
	assert.Equal(t, first.GetSession(), strings.TrimSpace(string(kept)))
	kept, err = os.ReadFile(filepath.Join(home, claudeSessionFile))
	require.NoError(t, err)
	assert.Equal(t, alone.GetSession(), strings.TrimSpace(string(kept)))

	// session and am, asked of the element, answer the element's session.
	assert.Equal(t, []string{"what is this?", "and this?"}, toldIn(t, s, sigil.Sent{"element": "agent-element"}))
	assert.Equal(t, []string{"hello"}, toldIn(t, s, nil))
	is, refused := s.harnessAm(s.claudeHarness(), sigil.Sent{"element": "agent-element"})
	require.Nil(t, refused)
	assert.Equal(t, first.GetSession(), is.(*protocol.ClaudeAm).GetSession())
	is, refused = s.harnessAm(s.claudeHarness(), nil)
	require.Nil(t, refused)
	assert.Equal(t, alone.GetSession(), is.(*protocol.ClaudeAm).GetSession())

	// The row of what was said carries the element it was said in.
	told := toldRows(t, s, first.GetSession())
	require.Len(t, told, 2)
	for _, row := range told {
		assert.Equal(t, "agent-element", row.Attributes["element"])
	}
	for _, row := range toldRows(t, s, alone.GetSession()) {
		assert.NotContains(t, row.Attributes, "element")
	}
}

// toldIn is what was said to the agent in the session named, as agent session reads it.
func toldIn(t *testing.T, s *QNTXServer, sent sigil.Sent) []string {
	t.Helper()
	answer, refused := s.harnessSession(s.claudeHarness(), sent)
	require.Nil(t, refused)
	var said []string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		if turn.GetSpeaker() == "human" {
			said = append(said, turn.GetText())
		}
	}
	return said
}

// toldRows is each row of what was said to the Node Agent in session, as it wrote them down.
func toldRows(t *testing.T, s *QNTXServer, session string) []*types.As {
	t.Helper()
	told, err := s.held.TheNodesOwnRecords().GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Contexts: []string{"session:" + session}, Predicates: []string{"UserPromptSubmit"}, Limit: 5})
	require.NoError(t, err)
	return told
}

// An element is kept under the agent's home by its id, so an id that is a path
// is not an element.
func TestAnElementIsNoPath(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	for _, element := range []string{"../root", "a/b", ".", ".."} {
		_, refused := saying(s, sigil.Sent{"says": "hello", "element": element})
		require.NotNil(t, refused, element)
		assert.Equal(t, sigil.Invalid, refused.GetWhy(), element)
		assert.Equal(t, "element", refused.GetParam(), element)
	}
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run for an element that is a path")
}

// "and its not the user Feedback form but literally an agent prompt, with the
// screenshot pasted right there."
// The screenshot uploaded to /api/files is put where the agent works, and the
// prompt says where it is and to read it (ADR-054).
func TestAShakenScreenshotIsPutWhereTheAgentReadsIt(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	s.dbPath = filepath.Join(t.TempDir(), "qntx.db")
	files, err := s.filesDir()
	require.NoError(t, err)
	const id = "0b6e9a3c-1f5d-4c2e-9a7b-2d3e4f5a6b7c"
	require.NoError(t, os.WriteFile(filepath.Join(files, id+".png"), []byte("PNG bytes"), 0o644))

	answer, refused := saying(s, sigil.Sent{"says": "why is this red?", "element": "agent-element", "file_id": id})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())

	at := filepath.Join(s.nodeAgent.home, "work", "shake", id+".png")
	copied, err := os.ReadFile(at)
	require.NoError(t, err)
	assert.Equal(t, "PNG bytes", string(copied))
	// The stand-in writes one argument per line, and this one spans three.
	args, err := os.ReadFile(filepath.Join(ran, "0", "args"))
	require.NoError(t, err)
	assert.Contains(t, string(args), "-p\nwhy is this red?\n\nA screenshot of element agent-element is at "+at+". Read it.\n--output-format\n")

	// What was said is written down as it was said, and where the screenshot went.
	told := toldRows(t, s, answer.GetSession())
	require.Len(t, told, 1)
	assert.Equal(t, "why is this red?", told[0].Attributes["prompt"])
	assert.Equal(t, "agent-element", told[0].Attributes["element"])
	assert.Equal(t, at, told[0].Attributes["screenshot"])
}

// "say being able to carry an image sounds usefull to me, and being able to
// attach an imagine whne in a mobile chat"
// A picture attached in a chat with no element is carried the same way.
func TestAPictureAttachedInChatIsCarriedToTheAgent(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	s.dbPath = filepath.Join(t.TempDir(), "qntx.db")
	files, err := s.filesDir()
	require.NoError(t, err)
	const id = "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f"
	require.NoError(t, os.WriteFile(filepath.Join(files, id+".png"), []byte("PNG bytes"), 0o644))

	answer, refused := saying(s, sigil.Sent{"says": "what is in this?", "file_id": id})
	require.Nil(t, refused)

	at := filepath.Join(s.nodeAgent.home, "work", "shake", id+".png")
	_, err = os.Stat(at)
	require.NoError(t, err)
	args, err := os.ReadFile(filepath.Join(ran, "0", "args"))
	require.NoError(t, err)
	assert.Contains(t, string(args), "-p\nwhat is in this?\n\nA picture is at "+at+". Read it.\n--output-format\n")

	told := toldRows(t, s, answer.GetSession())
	require.Len(t, told, 1)
	assert.Equal(t, at, told[0].Attributes["screenshot"])
	assert.NotContains(t, told[0].Attributes, "element")
}

// A picture the node does not hold is no picture.
func TestAPictureIsOneTheNodeHolds(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	s.dbPath = filepath.Join(t.TempDir(), "qntx.db")

	_, refused := saying(s, sigil.Sent{"says": "why?", "element": "agent-element", "file_id": "0b6e9a3c-1f5d-4c2e-9a7b-2d3e4f5a6b7c"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Equal(t, "file_id", refused.GetParam())

	_, refused = saying(s, sigil.Sent{"says": "why?", "element": "agent-element", "file_id": "../qntx.db"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
	assert.Equal(t, "file_id", refused.GetParam())

	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run with no screenshot to read")
}
