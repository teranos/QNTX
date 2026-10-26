package server

// What names a person is a fixture before it leaves the node (fixtures.go).
// Tim: the Node Agent reads fixtures, a body is written back, a push is marked.
// Spike: a USER reads the answer as it is; a scanner that did not run hands nothing on.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// standInScanner is a scanner for a test: the one address it knows becomes
// the first fixture, and it says so.
func standInScanner(ctx context.Context, text string) (string, string, error) {
	real := "someone" + "@" + "gmail.com"
	if !strings.Contains(text, real) {
		return text, "", nil
	}
	return strings.ReplaceAll(text, real, "tim@example.com"), "1 mail address", nil
}

// fixturesServer is a node holding the App on teranos/elements, whose GitHub
// is a stand-in that records what it is asked to write.
func fixturesServer(t *testing.T) (*QNTXServer, *[]string) {
	t.Helper()
	s := githubKnowingServer(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s.authHandler.SetGitHubApp("Iv23li-the-app", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	s.scrubWith = standInScanner

	written := &[]string{}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := r.Method + " " + r.URL.Path
		switch {
		case route == "GET /repos/teranos/elements/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": "the-app"})
		case route == "POST /app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_minted", "expires_at": "2099-01-01T00:00:00Z",
				"permissions": map[string]string{"contents": "write", "pull_requests": "write", "statuses": "write"}})
		case strings.HasPrefix(route, "GET /repos/teranos/elements/compare/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ahead", "files": []map[string]any{
				{"filename": "examples/late-table.ts", "status": "added", "patch": "@@ -0,0 +1,2 @@\n+const USERS = [\n+  ['someone" + "@" + "gmail.com'],\n"},
				{"filename": "table.ts", "status": "modified", "patch": "@@ -1 +1 @@\n-a\n+b\n"},
			}})
		case route == "PATCH /repos/teranos/elements/pulls/72", route == "PATCH /repos/teranos/elements/issues/comments/9", route == "POST /repos/teranos/elements/statuses/abc123":
			*written = append(*written, route+" "+string(body))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "number": 72, "state": "failure", "html_url": "https://github.com/teranos/elements/pull/72"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found: " + route})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s, written
}

func agentAdmission(label string) auth.Admission {
	a := auth.Admitted(auth.LevelRoot)
	a.Grant = &auth.Grant{Label: label, DID: "did:key:z6Mkagent"}
	return a
}

func answerOf(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, StructuredContent: json.RawMessage(`{"mail":"` + text + `"}`)}
}

func spoken(r *mcp.CallToolResult) string { return r.Content[0].(*mcp.TextContent).Text }

// "make sure the model never sees it"
func TestTimTheNodeAgentReadsFixtures(t *testing.T) {
	s, _ := fixturesServer(t)
	real := "someone" + "@" + "gmail.com"

	agent := s.fixtured(context.Background(), agentAdmission(auth.NodeAgentLabel), answerOf(real))
	assert.Equal(t, "tim@example.com", spoken(agent))
	assert.JSONEq(t, `{"mail":"tim@example.com"}`, string(agent.StructuredContent.(json.RawMessage)))

	element := s.fixtured(context.Background(), agentAdmission(auth.ElementAgentLabel+":users"), answerOf(real))
	assert.Equal(t, "tim@example.com", spoken(element))
}

func TestSpikeAUserReadsTheAnswerAsItIs(t *testing.T) {
	s, _ := fixturesServer(t)
	real := "someone" + "@" + "gmail.com"
	user := auth.Admitted(auth.LevelUser)
	assert.Equal(t, real, spoken(s.fixtured(context.Background(), user, answerOf(real))))
	assert.Nil(t, s.fixtured(context.Background(), agentAdmission(auth.NodeAgentLabel), nil))
}

func TestSpikeAScannerThatDidNotRunHandsNothingOn(t *testing.T) {
	s, _ := fixturesServer(t)
	s.scrubWith = nil
	// No agent of this node has a built ground, so there is no scanner.
	answered := s.fixtured(context.Background(), agentAdmission(auth.NodeAgentLabel), answerOf("anything"))
	assert.True(t, answered.IsError)
	assert.Contains(t, spoken(answered), "could not be made into fixtures")
	assert.Contains(t, spoken(answered), "no scanner")
}

func TestTimAPullRequestsBodyIsWrittenBackAsFixtures(t *testing.T) {
	s, written := fixturesServer(t)
	body := `{"action":"opened","number":72,"pull_request":{"title":"A table on a phone","body":"rows for someone` + "@" + `gmail.com"},"repository":{"full_name":"teranos/elements"}}`
	touched, err := s.fixturesFromGitHub(context.Background(), "pull_request", []byte(body))
	require.NoError(t, err)
	assert.Equal(t, []string{"teranos/elements #72"}, touched)
	require.Len(t, *written, 1)
	assert.Contains(t, (*written)[0], "PATCH /repos/teranos/elements/pulls/72")
	assert.Contains(t, (*written)[0], "rows for tim@example.com")
	assert.NotContains(t, (*written)[0], "gmail")

	where := auth.NamespaceDefault
	if s.held.KeepsSystem() {
		where = auth.NamespaceSystem
	}
	reading, err := s.held.Read(where)
	require.NoError(t, err)
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{fixturesPredicate}, Limit: ats.EveryRow})
	require.NoError(t, err)
	require.Len(t, lines, 1, "the line is written down")
	assert.Equal(t, "teranos/elements #72", lines[0].Subjects[0])
	assert.Equal(t, "1 mail address", lines[0].Attributes["found"])
}

func TestTimACommentIsWrittenBackAsFixtures(t *testing.T) {
	s, written := fixturesServer(t)
	body := `{"action":"created","issue":{"number":26},"comment":{"id":9,"body":"mail someone` + "@" + `gmail.com"},"repository":{"full_name":"teranos/elements"}}`
	touched, err := s.fixturesFromGitHub(context.Background(), "issue_comment", []byte(body))
	require.NoError(t, err)
	assert.Equal(t, []string{"teranos/elements #26 comment 9"}, touched)
	require.Len(t, *written, 1)
	assert.Contains(t, (*written)[0], "PATCH /repos/teranos/elements/issues/comments/9")
	assert.Contains(t, (*written)[0], "mail tim@example.com")
}

func TestTimAPushThatAddsALineNamingAPersonIsMarkedFailed(t *testing.T) {
	s, written := fixturesServer(t)
	body := `{"ref":"refs/heads/root/table","before":"000aaa","after":"abc123","repository":{"full_name":"teranos/elements","default_branch":"main"},"commits":[]}`
	touched, err := s.fixturesFromGitHub(context.Background(), "push", []byte(body))
	require.NoError(t, err)
	assert.Equal(t, []string{"teranos/elements@abc123"}, touched)
	require.Len(t, *written, 1)
	assert.Contains(t, (*written)[0], "POST /repos/teranos/elements/statuses/abc123")
	assert.Contains(t, (*written)[0], `"state":"failure"`)
	assert.Contains(t, (*written)[0], `"context":"fixtures"`)
	assert.Contains(t, (*written)[0], "examples/late-table.ts: 1 mail address")
	assert.NotContains(t, (*written)[0], "; table.ts", "a file that adds nothing of the kind is not named")
}

func TestSpikeWordsWithNothingInThemTouchNothing(t *testing.T) {
	s, written := fixturesServer(t)
	body := `{"action":"opened","number":72,"pull_request":{"title":"A table","body":"rows for tim@example.com"},"repository":{"full_name":"teranos/elements"}}`
	touched, err := s.fixturesFromGitHub(context.Background(), "pull_request", []byte(body))
	require.NoError(t, err)
	assert.Empty(t, touched)
	assert.Empty(t, *written)

	touched, err = s.fixturesFromGitHub(context.Background(), "pull_request", []byte(strings.Replace(body, `"opened"`, `"closed"`, 1)))
	require.NoError(t, err)
	assert.Empty(t, touched)
}

func TestAddedLinesAreThePlusLinesAlone(t *testing.T) {
	assert.Equal(t, "b\n", addedLines("--- a\n+++ b\n@@ -1 +1 @@\n-a\n+b\n"))
	assert.Equal(t, "", addedLines(""))
}
