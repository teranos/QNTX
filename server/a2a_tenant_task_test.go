package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "Each agent it hosts is described by an Agent Card and reached by its
// tenant" (ADR-048): a message at a tenant is a Task of the agent there, its
// lines where that agent's session is.

// askedOverA2A is an A2A request to the binding as admitted, past the gate, and
// what it answered.
func askedOverA2A(t *testing.T, s *QNTXServer, admitted auth.Admission, method, path, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if body != "" {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	r.Header.Set("A2A-Version", "1.0")
	r = r.WithContext(auth.WithAdmission(r.Context(), admitted))
	w := httptest.NewRecorder()
	s.a2aHTTP().ServeHTTP(w, r)
	var answered map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	return w.Code, answered
}

// A message at garden is a Task of garden's agent: its lines in garden, where
// its session is written, and none of it the Node Agent's.
func TestAMessageAtANamespaceIsATaskOfItsAgent(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)
	_, refused := rootAsksAm(s, "set", sonnetGarden)
	require.Nil(t, refused)
	begun, refused := rootAsks(s, "login", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	require.NotEmpty(t, begun["url"])
	_, refused = rootAsks(s, "login", sigil.Sent{"namespace": "garden", "code": "the-code-shown"})
	require.Nil(t, refused)
	underGround(t, filepath.Join(s.agentsDir, "garden", "claude-sonnet-5-5"))
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = "https://example.org/root"

	code, sent := askedOverA2A(t, s, root, http.MethodPost, "/a2a/garden/message:send", saidToTheNode("how long has the box been up?", ""))
	require.Equal(t, http.StatusOK, code, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(sent, "task.status.state"))
	assert.Equal(t, "Up 3 days.", at(sent, "task.artifacts.0.parts.0.text"))
	id := at(sent, "task.id").(string)

	var lines int
	for _, row := range in.store["garden"].rows {
		if len(row.Subjects) == 1 && row.Subjects[0] == taskLineSubject {
			lines++
		}
	}
	assert.Equal(t, 3, lines, "submitted, working and completed, in garden")
	system, _ := s.held.WriteWhatTheNodeKnowsOfItself()
	assert.Empty(t, system.(*handed).rows, "a Task of garden's agent landed in system")

	code, got := askedOverA2A(t, s, root, http.MethodGet, "/a2a/garden/tasks/"+id, "")
	require.Equal(t, http.StatusOK, code, got)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(got, "status.state"))
	code, _ = askedOverA2A(t, s, root, http.MethodGet, "/a2a/tasks/"+id, "")
	assert.Equal(t, http.StatusNotFound, code, "garden's Task is no Task of the Node Agent")

	_, listed := askedOverA2A(t, s, root, http.MethodGet, "/a2a/garden/tasks", "")
	assert.Equal(t, float64(1), at(listed, "totalSize"))
	_, listed = askedOverA2A(t, s, root, http.MethodGet, "/a2a/tasks", "")
	assert.Equal(t, float64(0), at(listed, "totalSize"))
}

// "AGENT PER ELEMENT, EACH ELEMENT IS ITS OWN CONVERSATION" (ADR-054): an
// element's agent is described and reached at element:<element>, its Tasks its
// own beside the Node Agent's in system.
func TestAMessageAtAnElementIsATaskOfItsAgent(t *testing.T) {
	namedNode(t)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	keyedNode(srv)

	code, card := a2aAsked(t, srv, http.MethodGet, "/a2a/element:approvals-element/extendedAgentCard", "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, card)
	assert.Equal(t, "the element approvals-element", at(card, "name"))
	assert.Equal(t, "element:approvals-element", at(card, "supportedInterfaces.0.tenant"))
	assert.Equal(t, "say", at(card, "skills.0.id"))

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/element:approvals-element/message:send", saidToTheNode("what waits on me?", ""), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(sent, "task.status.state"))
	id := at(sent, "task.id").(string)

	_, listed := a2aAsked(t, srv, http.MethodGet, "/a2a/element:approvals-element/tasks", "", tokens[auth.LevelRoot])
	assert.Equal(t, float64(1), at(listed, "totalSize"))
	assert.Equal(t, id, at(listed, "tasks.0.id"))
	_, listed = a2aAsked(t, srv, http.MethodGet, "/a2a/element:another-element/tasks", "", tokens[auth.LevelRoot])
	assert.Equal(t, float64(0), at(listed, "totalSize"), "one element's Task is another's")
	code, _ = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusNotFound, code, "an element's Task is no Task of the Node Agent")

	// An element named by a path is no element.
	code, _ = a2aAsked(t, srv, http.MethodGet, "/a2a/element:..%2Fsecrets/tasks", "", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusNotFound, code)
}
