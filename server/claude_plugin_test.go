package server

import (
	"context"
	"net"
	"os"
	"testing"

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
	assert.Equal(t, "how long has the box been up?", args[len(args)-1])
	ranHere, err := os.ReadDir(nodeRan)
	require.NoError(t, err)
	assert.Empty(t, ranHere, "the node ran its own Claude Code while the plugin runs it")

	said := sessionSaid(t, s)
	assert.Contains(t, said, [2]string{"human", "how long has the box been up?"})
}
