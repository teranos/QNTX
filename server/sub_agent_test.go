package server

import (
	"context"
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
	"github.com/teranos/QNTX/internal/sacred"
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
	held := &claudeCodeHeld{fetched: make(chan struct{})}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := held.Path(gone)
	require.Error(t, err)
}

// A node whose am.toml names no agent fetches nothing.
func TestANodeThatNamesNoAgentFetchesNoClaudeCode(t *testing.T) {
	s := &QNTXServer{deps: &serverDependencies{cfg: &appcfg.Config{}}, logger: zaptest.NewLogger(t).Sugar()}
	require.NoError(t, agentSubsystem{}.Init(s))
	assert.Nil(t, s.claudeCode)
}
