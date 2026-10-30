package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/encoding/protojson"
)

// Each line of the stream is said in the words every harness has one of, and
// carried whole as raw: nothing the harness printed is lost in the saying.
func TestALineOfTheStreamIsSaidAsEventsAndCarriedWhole(t *testing.T) {
	at := time.Date(2026, 10, 28, 21, 0, 0, 0, time.UTC)
	lines := map[string]struct {
		kinds []string
		text  string
	}{
		`{"type":"system","subtype":"init","model":"claude-opus-5-5"}`:                                                                                             {[]string{"started"}, "claude-opus-5-5"},
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Up 3"}}}`:                                     {[]string{"delta"}, "Up 3"},
		`{"type":"stream_event","event":{"type":"message_start","message":{}}}`:                                                                                    {[]string{"other"}, ""},
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Checking."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"uptime"}}]}}`: {[]string{"text", "tool_use"}, "Checking."},
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"3 days"}]}}`:                                                     {[]string{"tool_result"}, "3 days"},
		`{"type":"result","subtype":"success","is_error":false,"result":"Up 3 days."}`:                                                                             {[]string{"ended"}, "Up 3 days."},
		`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"too many"}`:                                                                        {[]string{"failed"}, "error_max_turns: too many"},
		`not json`: {[]string{"other"}, ""},
	}
	for line, want := range lines {
		events := sessionEventsOf("s-1", "claude", []byte(line), 42, at)
		kinds := make([]string, 0, len(events))
		for _, event := range events {
			kinds = append(kinds, event.Kind)
			assert.Equal(t, line, event.Raw, line)
			assert.Equal(t, int64(42), event.Mark, line)
			assert.Equal(t, "s-1", event.Session, line)
		}
		assert.Equal(t, want.kinds, kinds, line)
		assert.Equal(t, want.text, events[0].Text, line)
	}
	tool := sessionEventsOf("s-1", "claude", []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"uptime"}}]}}`), 1, at)[0]
	assert.Equal(t, "Bash", tool.Tool)
	assert.Equal(t, "t1", tool.ToolUseId)
	assert.JSONEq(t, `{"command":"uptime"}`, tool.Input)
}

// What the live session answers while the Node Agent's turn prints: the words
// arrive on the wire before the result exists in the stream.
func TestTheLiveSessionHandsTheWordsBeforeTheTurnEnds(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	home := s.nodeAgent.home
	require.NoError(t, os.MkdirAll(filepath.Join(home, "turns"), 0o700))
	stream := filepath.Join(home, "turns", "s-live.1791900000000000000.stream")
	init := `{"type":"system","subtype":"init","session_id":"s-live","model":"m"}` + "\n"
	require.NoError(t, os.WriteFile(stream, []byte(init), 0o600))
	require.NoError(t, os.WriteFile(stream+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600))
	s.nodeAgent.in(s.claudeHarness()).answering.Store(&turnInSession{session: "s-live", stream: stream})
	appended, err := os.OpenFile(stream, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer appended.Close()

	served := httptest.NewServer(http.HandlerFunc(s.HandleNodeAgentLive))
	defer served.Close()
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	asked, err := http.NewRequestWithContext(ctx, http.MethodGet, served.URL+"/api/agent/live?harness=claude", nil)
	require.NoError(t, err)
	answer, err := http.DefaultClient.Do(asked)
	require.NoError(t, err)
	defer answer.Body.Close()
	require.Equal(t, http.StatusOK, answer.StatusCode)
	require.Equal(t, "text/event-stream", answer.Header.Get("Content-Type"))

	events := bufio.NewReader(answer.Body)
	next := func() (name string, event protocol.SessionEvent, end protocol.SessionEvents) {
		for {
			line, err := events.ReadString('\n')
			require.NoError(t, err)
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			case strings.HasPrefix(line, "data: "):
				data := []byte(strings.TrimPrefix(line, "data: "))
				if name == "end" {
					require.NoError(t, protojson.Unmarshal(data, &end))
				} else {
					require.NoError(t, protojson.Unmarshal(data, &event))
				}
				return
			}
		}
	}
	_, started, _ := next()
	assert.Equal(t, "started", started.Kind)

	delta := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Up"}}}` + "\n"
	_, err = appended.WriteString(delta)
	require.NoError(t, err)
	_, said, _ := next()
	// The words are on the wire, and the stream holds no result yet.
	assert.Equal(t, "delta", said.Kind)
	assert.Equal(t, "Up", said.Text)
	held, err := os.ReadFile(stream)
	require.NoError(t, err)
	assert.NotContains(t, string(held), `"type":"result"`)

	result := `{"type":"result","subtype":"success","session_id":"s-live","is_error":false,"result":"Up 3 days."}` + "\n"
	_, err = appended.WriteString(result)
	require.NoError(t, err)
	_, ended, _ := next()
	assert.Equal(t, "ended", ended.Kind)
	assert.Equal(t, "Up 3 days.", ended.Text)
	name, _, end := next()
	assert.Equal(t, "end", name)
	assert.False(t, end.Live)
	assert.Equal(t, int64(len(init)+len(delta)+len(result)), end.Mark)
}

// An element's agent is tailed at its own path, its session its own (ADR-054).
func TestAnElementAgentsLiveSessionIsItsOwn(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	s.nodeDID = &nodedid.Handler{PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))}
	agent, refused := s.holdElementAgent("agent-element")
	require.Nil(t, refused)
	stream := filepath.Join(agent.home, "turns", "s-el.1791900000000000000.stream")
	require.NoError(t, os.MkdirAll(filepath.Dir(stream), 0o700))
	require.NoError(t, os.WriteFile(stream, []byte(`{"type":"result","subtype":"success","session_id":"s-el","is_error":false,"result":"Shaken."}`+"\n"), 0o600))
	agent.in(s.claudeHarness()).answering.Store(&turnInSession{session: "s-el", stream: stream})

	asked := httptest.NewRequest(http.MethodGet, "/api/elements/agent-element/live?harness=claude", nil)
	asked.SetPathValue("element", "agent-element")
	recorded := httptest.NewRecorder()
	s.HandleElementLive(recorded, asked)
	assert.Equal(t, http.StatusOK, recorded.Code, recorded.Body.String())
	assert.Contains(t, recorded.Body.String(), `"kind":"ended"`)
	assert.Contains(t, recorded.Body.String(), `"session":"s-el"`)

	// The Node Agent is in no turn: its own live says so, by its own name.
	recorded = httptest.NewRecorder()
	s.HandleNodeAgentLive(recorded, httptest.NewRequest(http.MethodGet, "/api/agent/live?harness=claude", nil))
	assert.Equal(t, http.StatusNotFound, recorded.Code)
	assert.Contains(t, recorded.Body.String(), "the Node Agent is not in a turn")
}

// Between turns there is no stream to tail, and a harness that prints to no
// file is refused by name.
func TestTheLiveSessionRefusesWhatItCannotTail(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	for _, ask := range []struct{ query, says string }{
		{"harness=pi", "only Claude Code prints to one"},
		{"harness=claude", "not in a turn in Claude Code"},
		{"harness=claude&mark=x", "mark is where in the stream to read from"},
	} {
		recorded := httptest.NewRecorder()
		s.HandleNodeAgentLive(recorded, httptest.NewRequest(http.MethodGet, "/api/agent/live?"+ask.query, nil))
		assert.NotEqual(t, http.StatusOK, recorded.Code, ask.query)
		assert.Contains(t, recorded.Body.String(), ask.says, ask.query)
	}
}

// Every element agent the node holds is listed by the element's name, kept
// beside its sessions: "the Agents Element as the main entry-point into all
// Agents that User has access to."
func TestEveryElementAgentHeldIsListed(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	s.nodeDID = &nodedid.Handler{PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))}
	for _, element := range []string{"agent-element", "story-1-root-invites-a-user"} {
		_, refused := s.holdElementAgent(element)
		require.Nil(t, refused)
	}
	shaken, _ := s.holdElementAgent("agent-element")
	require.NoError(t, shaken.keepIn(filepath.Join(shaken.sessionDir, claudeSessionFile), "s-shaken"))

	answer, refused := s.elementsList(context.Background(), sigil.Sent{})
	require.Nil(t, refused)
	listed := answer.(*protocol.ElementAgents).Agents
	names := map[string]string{}
	for _, one := range listed {
		names[one.Element] = one.Session
	}
	assert.Equal(t, map[string]string{"agent-element": "s-shaken", "story-1-root-invites-a-user": ""}, names)
}

// An element whose agent runs on the box has no Session at the Console: sessions says none, and names no agent.
func TestAnElementOnTheBoxHasNoConsoleSessions(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	s.nodeDID = &nodedid.Handler{PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))}
	_, refused := s.holdElementAgent("agent-element")
	require.Nil(t, refused)
	answer, refused := s.elementSessions(context.Background(), sigil.Sent{"element": "agent-element"})
	require.Nil(t, refused)
	listed := answer.(*protocol.ElementSessions)
	assert.Equal(t, "", listed.Agent)
	assert.Empty(t, listed.Sessions)
}
