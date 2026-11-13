package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/harness/claudeharness"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// "Harness is a plugin that speaks grpc A2A"

// claudePluginRuns has a claude plugin running on s, whose Claude Code is the
// stand-in at binary: a2a-go's gRPC binding of claudeharness, served and
// dialed on loopback, as piPluginRuns does for Pi.
func claudePluginRuns(t *testing.T, s *QNTXServer, binary string) {
	t.Helper()
	listening, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serving := grpc.NewServer()
	a2agrpc.NewHandler(claudeharness.Handler(func(context.Context) (string, error) { return binary, nil })).RegisterWith(serving)
	go func() { _ = serving.Serve(listening) }()
	t.Cleanup(serving.Stop)
	conn, err := grpc.NewClient(listening.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	s.pluginRegistry = plugin.NewRegistry("0.1.0", zaptest.NewLogger(t).Sugar())
	require.NoError(t, s.pluginRegistry.Register(&piPlugin{fakePlugin: fakePlugin{name: agentLineClaude}, served: a2apb.NewA2AServiceClient(conn)}))
	s.pluginRegistry.MarkReady(agentLineClaude)
}

// With a claude plugin running, what is said to the Node Agent in Claude Code
// is run by the plugin's Claude Code, not the node's: its records are written
// down as before, and the answer is Claude Code's.
func TestWhatIsSaidToClaudeCodeIsRunByTheClaudePlugin(t *testing.T) {
	s, nodeRan := runningTheNodeAgent(t, opusLow)
	binary, pluginRan := claudeStandIn(t, claudeAnswered)
	claudePluginRuns(t, s, binary)

	answer, refused := saying(s, sigil.Sent{"says": "how long has the box been up?"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	assert.False(t, answer.GetIsError())
	assert.Equal(t, []string{"Write"}, answer.GetDenied())

	args := ranWith(t, pluginRan, "0", "args")
	assert.Equal(t, answer.GetSession(), after(args, "--session-id"))
	assert.Equal(t, "how long has the box been up?", after(args, "-p"))
	ranHere, err := os.ReadDir(nodeRan)
	require.NoError(t, err)
	assert.Empty(t, ranHere, "the node ran its own Claude Code while the plugin runs it")

	said := sessionSaid(t, s)
	assert.Contains(t, said, [2]string{"human", "how long has the box been up?"})
}

// A turn core gives up stops Claude Code in the plugin, as it stops Pi: one
// that does not run apart.
func TestATurnCoreGivesUpStopsClaudeCodeInThePlugin(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "pid")
	binary := filepath.Join(dir, "claude")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\necho $$ > '"+started+"'\nexec sleep 120\n"), 0o755))
	s := &QNTXServer{}
	claudePluginRuns(t, s, binary)
	backend, runs := s.harnessBackend(agentLineClaude)
	require.True(t, runs)

	asking, giveUp := context.WithCancel(context.Background())
	defer giveUp()
	gaveUp := make(chan error, 1)
	go func() {
		_, err := saidInBackend(asking, backend, "how long has the box been up?",
			claudeharness.Run{Home: filepath.Join(dir, "home"), Session: "s-1", Model: "claude-opus-5-5", Effort: "low", Mode: "dontAsk", Token: "the-plan-token"},
			func(a2asdk.Event) error { return nil })
		gaveUp <- err
	}()

	var running int
	require.Eventually(t, func() bool {
		select {
		case err := <-gaveUp:
			t.Fatalf("the turn ended before Claude Code was started: %v", err)
		default:
		}
		written, err := os.ReadFile(started)
		if err != nil {
			return false
		}
		running, err = strconv.Atoi(strings.TrimSpace(string(written)))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "Claude Code was not started")

	giveUp()
	require.Error(t, <-gaveUp)
	assert.Eventually(t, func() bool { return syscall.Kill(running, 0) != nil }, 10*time.Second, 50*time.Millisecond,
		"Claude Code runs on in the plugin after the node gave its turn up")
}
