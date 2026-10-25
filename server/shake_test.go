package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// runningElementAgents is a node running the Node Agent, with a key, a place
// for agents and a gate, so an element's agent can be held.
func runningElementAgents(t *testing.T) (*QNTXServer, string) {
	t.Helper()
	s, ran, _ := runningNamespaceAgents(t)
	return s, ran
}

// sayingTo says something to the element's agent, as ROOT at its path.
func sayingTo(s *QNTXServer, element string, sent sigil.Sent) (*protocol.ClaudeSaid, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/elements/"+element+"/say", nil)
	sent["element"], sent["harness"] = element, "claude"
	answer, refused := s.elementsSignum().Answers["say"](sigil.WithCaller(context.Background(), asked), sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(*protocol.NodeAgentSaid).GetClaude(), nil
}

// toldTo is what was said to the element's agent, as elements session reads it.
func toldTo(t *testing.T, s *QNTXServer, element string) []string {
	t.Helper()
	answer, refused := s.elementsSignum().Answers["session"](context.Background(), sigil.Sent{"element": element, "harness": "claude"})
	require.Nil(t, refused)
	var said []string
	for _, turn := range answer.(*protocol.SessionTranscript).GetTranscript().GetTurns() {
		if turn.GetSpeaker() == "human" {
			said = append(said, turn.GetText())
		}
	}
	return said
}

// toldRows is each row of what was said to an agent of ROOT's in session, as it wrote them down.
func toldRows(t *testing.T, s *QNTXServer, session string) []*types.As {
	t.Helper()
	told, err := s.held.TheNodesOwnRecords().GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Contexts: []string{"session:" + session}, Predicates: []string{"UserPromptSubmit"}, Limit: 5})
	require.NoError(t, err)
	return told
}

// "and its not the user Feedback form but literally an agent prompt, with the screenshot pasted right there."

// The screenshot uploaded to /api/files is put where the element's agent
// works, and the prompt says where it is and to read it (ADR-054).
func TestAShakenScreenshotIsPutWhereTheElementReadsIt(t *testing.T) {
	s, ran := runningElementAgents(t)
	home := s.nodeAgent.home
	s.dbPath = filepath.Join(t.TempDir(), "qntx.db")
	files, err := s.filesDir()
	require.NoError(t, err)
	const id = "0b6e9a3c-1f5d-4c2e-9a7b-2d3e4f5a6b7c"
	require.NoError(t, os.WriteFile(filepath.Join(files, id+".png"), []byte("PNG bytes"), 0o644))

	answer, refused := sayingTo(s, "note-1", sigil.Sent{"says": "why is this red?", "file_id": id})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())

	at := filepath.Join(home, "work", "shake", id+".png")
	copied, err := os.ReadFile(at)
	require.NoError(t, err)
	assert.Equal(t, "PNG bytes", string(copied))
	// The stand-in writes one argument per line, and this one spans three.
	args, err := os.ReadFile(filepath.Join(ran, theRunSaying(t, ran), "args"))
	require.NoError(t, err)
	assert.Contains(t, string(args), "-p\nwhy is this red?\n\nA screenshot of element note-1 is at "+at+". Read it.\n--output-format\n")

	// What was said is written down as it was said, and where the screenshot went.
	told := toldRows(t, s, answer.GetSession())
	require.Len(t, told, 1)
	assert.Equal(t, "why is this red?", told[0].Attributes["prompt"])
	assert.Equal(t, "note-1", told[0].Attributes["element"])
	assert.Equal(t, at, told[0].Attributes["screenshot"])
}

// "say being able to carry an image sounds usefull to me, and being able to attach an imagine whne in a mobile chat"

// A picture attached in the Node Agent's chat is carried the same way.
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

	_, refused := saying(s, sigil.Sent{"says": "why?", "file_id": "0b6e9a3c-1f5d-4c2e-9a7b-2d3e4f5a6b7c"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Equal(t, "file_id", refused.GetParam())

	_, refused = saying(s, sigil.Sent{"says": "why?", "file_id": "../qntx.db"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
	assert.Equal(t, "file_id", refused.GetParam())

	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run with no screenshot to read")
}

// "AGENT PER ELEMENT, EACH ELEMENT IS ITS OWN CONVERSATION"

// "DAY 3 SHAKE AGAIN, SAME ELEMENT"

// "YES, WHERE WE LEFT OFF, SEPARATE FROM THE NODE AGENT"
func TestEachElementIsItsOwnAgentAndItsOwnConversation(t *testing.T) {
	s, ran := runningElementAgents(t)

	day1, refused := sayingTo(s, "note-1", sigil.Sent{"says": "the note is too wide"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", day1.GetAnswer())
	day3, refused := sayingTo(s, "note-1", sigil.Sent{"says": "still too wide"})
	require.Nil(t, refused)
	other, refused := sayingTo(s, "ax-element", sigil.Sent{"says": "what is this?"})
	require.Nil(t, refused)
	node, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)

	// Day 3 goes on from day 1: one session, resumed.
	assert.Equal(t, day1.GetSession(), day3.GetSession())
	assert.NotEqual(t, day1.GetSession(), other.GetSession())
	assert.NotEqual(t, day1.GetSession(), node.GetSession())
	assert.NotEqual(t, other.GetSession(), node.GetSession())
	kept, err := os.ReadFile(filepath.Join(s.nodeAgent.home, "elements", "note-1", claudeSessionFile))
	require.NoError(t, err)
	assert.Equal(t, day1.GetSession(), strings.TrimSpace(string(kept)))
	resumed := false
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	for _, run := range runs {
		if after(ranWith(t, ran, run.Name(), "args"), "--resume") == day1.GetSession() {
			resumed = true
		}
	}
	assert.True(t, resumed, "day 3 did not resume day 1's session")

	// Each element is its own agent, its own key, told it is that element; it stands where the Node Agent stands, run as [agent.root] says.
	note, refused := s.holdElementAgent("note-1")
	require.Nil(t, refused)
	ax, refused := s.holdElementAgent("ax-element")
	require.Nil(t, refused)
	assert.NotEqual(t, note.did, ax.did)
	assert.NotEqual(t, note.did, s.nodeAgent.did)
	assert.Equal(t, s.nodeAgent.home, note.home)
	args := ranWith(t, ran, theRunSaying(t, ran), "args")
	assert.Contains(t, after(args, "--append-system-prompt"), "You are the element note-1")
	assert.Contains(t, after(args, "--append-system-prompt"), note.did)
	assert.Equal(t, opusLow.Model, after(args, "--model"))
	assert.Contains(t, ranWith(t, ran, theRunSaying(t, ran), "env"), "CLAUDE_CODE_OAUTH_TOKEN=the-plan-token")

	// Its conversation is the node's own record, and each row names the element.
	told := toldRows(t, s, day1.GetSession())
	require.Len(t, told, 2)
	for _, row := range told {
		assert.Equal(t, "note-1", row.Attributes["element"])
	}
	for _, row := range toldRows(t, s, node.GetSession()) {
		assert.NotContains(t, row.Attributes, "element")
	}
	assert.Equal(t, []string{"the note is too wide", "still too wide"}, toldTo(t, s, "note-1"))
}

// An element's agent is kept under the node's agents by the element's id, so
// an id that is a path is not an element.
func TestAnElementIsNoPath(t *testing.T) {
	s, ran := runningElementAgents(t)
	for _, element := range []string{"../root", "a/b", ".", ".."} {
		_, refused := sayingTo(s, element, sigil.Sent{"says": "hello"})
		require.NotNil(t, refused, element)
		assert.Equal(t, sigil.Invalid, refused.GetWhy(), element)
		assert.Equal(t, "element", refused.GetParam(), element)
	}
	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, runs, "Claude Code was run for an element that is a path")
}
