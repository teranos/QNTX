package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/claudecode"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap/zaptest"
)

// releasing is a release host holding one binary as version 9.9.9, and the
// pin that names it when pinned is the bytes the pin was made for.
func releasing(t *testing.T, served, pinned []byte) claudecode.Pin {
	t.Helper()
	platform, err := claudecode.Platform()
	require.NoError(t, err)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/9.9.9/"+platform+"/claude" {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(served); err != nil {
			t.Errorf("the binary was not served: %v", err)
		}
	}))
	t.Cleanup(host.Close)
	sum := sha256.Sum256(pinned)
	return claudecode.Pin{Base: host.URL, Version: "9.9.9", Checksums: map[string]string{platform: hex.EncodeToString(sum[:])}}
}

// The node gets its own Claude Code: nobody installs one on the machine it
// runs on (ADR-048).
func TestTheNodeFetchesTheClaudeCodeItPins(t *testing.T) {
	binary := []byte("#!/bin/sh\necho claude\n")
	dir := t.TempDir()
	var running sacred.Group

	held := holdClaudeCode(context.Background(), releasing(t, binary, binary), dir, running.Go, zaptest.NewLogger(t).Sugar())
	path, err := held.Path(context.Background())
	require.NoError(t, err)
	running.Wait()

	assert.Equal(t, filepath.Join(dir, "9.9.9", "claude"), path)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, binary, got)
}

// A binary that is not the one pinned is not kept, and whoever asks for it is
// told why there is none.
func TestAClaudeCodeThatIsNotThePinnedOneIsNotHeld(t *testing.T) {
	dir := t.TempDir()
	var running sacred.Group

	held := holdClaudeCode(context.Background(), releasing(t, []byte("something else"), []byte("the pinned one")), dir, running.Go, zaptest.NewLogger(t).Sugar())
	path, err := held.Path(context.Background())
	running.Wait()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256")
	assert.Empty(t, path)
}

// Asking does not wait past the asker's own patience for a fetch still going.
func TestAskingForClaudeCodeEndsWithTheAsker(t *testing.T) {
	held := &harnessHeld{fetched: make(chan struct{})}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := held.Path(gone)
	require.Error(t, err)
}

// The node names its ROOT agent at start, and writes its token down where the
// gate reads tokens: what the agent presents is admitted as ROOT's kind, and
// acts as the agent's own DID (ADR-048).
func TestTheNodeNamesItsRootAgentAndTheGateKnowsIt(t *testing.T) {
	_, db := createTestStore(t)
	tokens, _, err := auth.OpenTokenTable(db, nil)
	require.NoError(t, err)
	h, err := auth.New(db, "localhost", nil, 8770, 8820, 24, zaptest.NewLogger(t).Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, nil, false, []string{rootAccount}, nil)
	require.NoError(t, err)
	node := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	s := &QNTXServer{authHandler: h, nodeDID: &nodedid.Handler{PrivateKey: node}, logger: zaptest.NewLogger(t).Sugar()}

	require.NoError(t, s.nameRootAgent(t.TempDir()))
	require.NotNil(t, s.rootAgent)

	var admitted auth.Admission
	gated := h.Middleware("/mcp", auth.Reach{}, func(w http.ResponseWriter, r *http.Request) {
		admitted, _ = auth.AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	asked := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	asked.Header.Set("Authorization", "Bearer "+s.rootAgent.token)
	answered := httptest.NewRecorder()
	gated.ServeHTTP(answered, asked)

	require.Equal(t, http.StatusOK, answered.Code)
	assert.True(t, admitted.IsRoot())
	assert.Equal(t, s.rootAgent.did, admitted.ActsAs())

	// A second start finds the token held, and holds it once.
	require.NoError(t, s.nameRootAgent(t.TempDir()))
}

// A node with no login has no gate to know the agent, and names it all the same.
func TestANodeWithNoLoginStillNamesItsRootAgent(t *testing.T) {
	node := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	s := &QNTXServer{nodeDID: &nodedid.Handler{PrivateKey: node}, logger: zaptest.NewLogger(t).Sugar()}
	require.NoError(t, s.nameRootAgent(t.TempDir()))
	require.NotNil(t, s.rootAgent)
	assert.NotEmpty(t, s.rootAgent.did)
}

// Where the node answers on its own machine is where its agent reaches it.
func TestWhereTheNodeAnswersOnItsOwnMachine(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8770", ownURLOf("0.0.0.0", 8770))
	assert.Equal(t, "http://127.0.0.1:8770", ownURLOf("", 8770))
	assert.Equal(t, "http://127.0.0.1:8770", ownURLOf("127.0.0.1", 8770))
	assert.Equal(t, "http://[::1]:8770", ownURLOf("::", 8770))
	assert.Equal(t, "http://10.0.0.5:8770", ownURLOf("10.0.0.5", 8770))
}

// What is fetched already is said at once and every time, however little
// patience whoever asks has: on the box it was said to be still arriving.
func TestAClaudeCodeAlreadyFetchedIsNeverSaidToBeArriving(t *testing.T) {
	fetched := make(chan struct{})
	close(fetched)
	held := &harnessHeld{fetched: fetched, path: "/kept/claude"}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	for range 200 {
		path, arrived, err := held.Now()
		require.NoError(t, err)
		require.True(t, arrived)
		require.Equal(t, "/kept/claude", path)

		path, err = held.Path(gone)
		require.NoError(t, err)
		require.Equal(t, "/kept/claude", path)
	}

	_, arrived, err := (&harnessHeld{fetched: make(chan struct{})}).Now()
	require.NoError(t, err)
	assert.False(t, arrived)
}

// A node whose am.toml names no agent fetches nothing.
func TestANodeThatNamesNoAgentFetchesNoClaudeCode(t *testing.T) {
	s := &QNTXServer{deps: &serverDependencies{cfg: &appcfg.Config{}}, logger: zaptest.NewLogger(t).Sugar()}
	require.NoError(t, agentSubsystem{}.Init(s))
	assert.Nil(t, s.harnessHeldBy("claude"))
}
