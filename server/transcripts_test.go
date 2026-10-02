package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap/zaptest"
)

// One event as Ground streams it (ground source/stream.d): subject is
// parent/repo:branch, context the session, and a tool call carries only
// tool_name, file_path and command.
func streamed(id, predicate, session string, at time.Time, attrs map[string]any) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{"user/QNTX:datapunt-owns-the-reference"},
		Predicates: []string{predicate},
		Contexts:   []string{"session:" + session},
		Actors:     []string{"ground"},
		Source:     "ground ",
		Timestamp:  at,
		CreatedAt:  at,
		Attributes: attrs,
	}
}

func aSession(at time.Time) []*types.As {
	return []*types.As{
		streamed("ground:payload:UserPromptSubmit:1", "UserPromptSubmit", "s-1", at,
			map[string]any{"prompt": "Build QNTX here"}),
		streamed("ground:payload:PreToolUse:2", "PreToolUse", "s-1", at.Add(time.Second),
			map[string]any{"tool_name": "Bash", "file_path": nil, "command": "make cli", "original_size": 120}),
		streamed("ground:payload:GroundedPreToolUse:3", "GroundedPreToolUse", "s-1", at.Add(2*time.Second),
			map[string]any{"control": "no-comment-blocks"}),
		streamed("ground:payload:PreToolUse:4", "PreToolUse", "s-1", at.Add(3*time.Second),
			map[string]any{"tool_name": "Read", "file_path": "/home/user/QNTX/Makefile", "command": nil}),
		streamed("ground:payload:PreToolUse:5", "PreToolUse", "s-1", at.Add(4*time.Second),
			map[string]any{"tool_name": "Grep", "file_path": nil, "command": nil}),
		streamed("ritual:rite:grove-1:built:6", "ritual:rite", "s-1", at.Add(5*time.Second),
			map[string]any{"rite": "built", "verdict": "advance", "code": float64(0)}),
		streamed("ground:payload:Stop:7", "Stop", "s-1", at.Add(6*time.Second),
			map[string]any{"last_assistant_message": "QNTX builds here."}),
	}
}

// Tim: a session reads back as what was said and done, in order, each turn
// naming the attestation it came from.
func TestATranscriptIsTheSessionInOrder(t *testing.T) {
	at := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	got := transcriptsOf(aSession(at), transcriptSessionLimit)
	require.Len(t, got, 1)
	tr := got[0]
	assert.Equal(t, "s-1", tr.Session)
	assert.Equal(t, []string{"user/QNTX:datapunt-owns-the-reference"}, tr.Subjects)
	assert.Equal(t, "2026-10-01T22:00:00Z", tr.Started)
	assert.Equal(t, "2026-10-01T22:00:06Z", tr.Ended)

	var said [][2]string
	for _, turn := range tr.Turns {
		said = append(said, [2]string{turn.Speaker, turn.Text})
	}
	assert.Equal(t, [][2]string{
		{"human", "Build QNTX here"},
		{"tool", "make cli"},
		{"ground", "no-comment-blocks on PreToolUse"},
		{"read", "/home/user/QNTX/Makefile"},
		{"search", "Grep"},
		{"rite", "built advance 0"},
		{"assistant", "QNTX builds here."},
	}, said)
	assert.Equal(t, "ground:payload:UserPromptSubmit:1", tr.Turns[0].Of)
}

// Spike: an event with no session, or one a transcript does not read, is not
// a turn; sessions come newest first and stop at the limit.
func TestTranscriptsAreNewestFirstAndCut(t *testing.T) {
	at := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	older := streamed("a", "UserPromptSubmit", "older", at, map[string]any{"prompt": "first"})
	newer := streamed("b", "UserPromptSubmit", "newer", at.Add(time.Hour), map[string]any{"prompt": "second"})
	stray := streamed("c", "UserPromptSubmit", "", at, map[string]any{"prompt": "nowhere"})
	stray.Contexts = []string{"project:teranos/QNTX"}
	other := streamed("d", "immediate:ci-status", "older", at, map[string]any{"detail": "Checking CI..."})

	got := transcriptsOf([]*types.As{older, newer, stray, other, older}, 10)
	require.Len(t, got, 2)
	assert.Equal(t, "newer", got[0].Session)
	assert.Equal(t, "older", got[1].Session)
	require.Len(t, got[1].Turns, 1)

	assert.Len(t, transcriptsOf([]*types.As{older, newer}, 1), 1)
	assert.Empty(t, transcriptsOf(nil, 10))
}

// Spike: a sigma (ADR-020) lists hook predicates but holds no turn. It is
// counted as folded, never read as an empty turn.
func TestAFoldedSessionSaysSo(t *testing.T) {
	at := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	sigma := streamed("AS-distill-1", "PreToolUse", "s-1", at.Add(time.Hour), map[string]any{
		"_distill": true, "_total": float64(46),
		"_first_seen": "2026-10-01T21:00:00+00:00", "_last_seen": "2026-10-01T23:30:00+00:00",
	})
	sigma.Predicates = []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"}
	sigma.Source = "distill"

	got := transcriptsOf(append(aSession(at), sigma), 10)
	require.Len(t, got, 1)
	assert.Equal(t, 46, got[0].Folded)
	assert.Len(t, got[0].Turns, 7)
	assert.Equal(t, "2026-10-01T21:00:00Z", got[0].Started, "what was folded began before what is left")
	assert.Equal(t, "2026-10-01T23:30:00Z", got[0].Ended)

	only := transcriptsOf([]*types.As{sigma}, 10)
	require.Len(t, only, 1)
	assert.Equal(t, 46, only[0].Folded)
	assert.Empty(t, only[0].Turns)
	assert.Equal(t, "2026-10-01T23:30:00Z", only[0].Ended)
}

// Jenny: the sigil reads the namespace the caller stands in, and one session
// can be asked for by its id.
func TestTheTranscriptsSigilReadsWhereTheCallerStands(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	at := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	for _, as := range append(aSession(at), streamed("x", "UserPromptSubmit", "s-2", at.Add(time.Hour), map[string]any{"prompt": "another"})) {
		require.NoError(t, store.CreateAttestation(as))
	}
	s := &QNTXServer{held: servingOne(db, store), logger: zaptest.NewLogger(t).Sugar()}
	signum := s.transcriptsSignum()
	require.NoError(t, signum.Check())
	asked := httptest.NewRequest(http.MethodGet, "/api/transcripts", nil)

	answer, refused := signum.Answers["read"](sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	holds(t, signum, "read", answer)
	all := answer.(map[string]any)["transcripts"].([]transcript)
	require.Len(t, all, 2)
	assert.Equal(t, "s-2", all[0].Session)
	assert.Len(t, all[1].Turns, 7)

	answer, refused = signum.Answers["read"](sigil.WithCaller(context.Background(), asked), sigil.Sent{"session": "s-1"})
	require.Nil(t, refused)
	one := answer.(map[string]any)["transcripts"].([]transcript)
	require.Len(t, one, 1)
	assert.Equal(t, "s-1", one[0].Session)
}

func TestTranscriptsAreRootsAlone(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	assert.Equal(t, []string{"ROOT"}, compiled["/api/transcripts"])
}
