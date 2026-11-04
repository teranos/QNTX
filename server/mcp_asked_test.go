package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// "as part of 6SION, we want to have more insights and observability into what happened to the session that was working on it"
//
// A tool call that was answered leaves one line: the tool, what its sigil says
// of it, how long each part took, what was written back, and the traceparent
// Anthropic sent with it.
func TestAnMCPToolCallLeavesItsLine(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	asked := &mcpAsked{arrived: time.Now(), logger: zap.New(core).Sugar()}
	handler := asked.recording("github_ask", func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpAskedOf(ctx).says("operation", "GetABranch")
		mcpAskedOf(ctx).now(mcpMakingFixtures)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "answered"}}}, nil
	})

	traceparent := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	result, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{"traceparent": traceparent}}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	wrote := &writtenBack{ResponseWriter: httptest.NewRecorder()}
	_, err = wrote.Write([]byte("answered"))
	require.NoError(t, err)
	asked.ends(wrote)

	said := logs.FilterMessage("an MCP tool call was answered").All()
	require.Len(t, said, 1)
	line := said[0].ContextMap()
	assert.Equal(t, "github_ask", line["tool"])
	assert.Equal(t, "GetABranch", line["operation"])
	assert.Equal(t, traceparent, line["traceparent"])
	assert.Equal(t, true, line["handed"])
	assert.Equal(t, mcpWritingBack, line["in"])
	assert.EqualValues(t, 8, line["answer_chars"])
	assert.EqualValues(t, 8, line["written_bytes"])
	assert.Contains(t, line, mcpAsking+"_ms")
	assert.Contains(t, line, mcpMakingFixtures+"_ms")
}

// A call whose request ended before it was answered says what it was in, and
// an answer that comes after says so too. A request that carried no tool call
// says nothing.
func TestAnMCPToolCallNobodyAnsweredSaysWhatItWasIn(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	asked := &mcpAsked{arrived: time.Now(), logger: zap.New(core).Sugar()}
	asked.begins("github_ask", "")
	asked.now(mcpMakingFixtures)
	asked.ends(&writtenBack{ResponseWriter: httptest.NewRecorder()})

	said := logs.FilterMessage("an MCP tool call's request ended before it was answered").All()
	require.Len(t, said, 1)
	assert.Equal(t, mcpMakingFixtures, said[0].ContextMap()["in"])
	assert.Equal(t, false, said[0].ContextMap()["handed"])

	asked.hands(&mcp.CallToolResult{}, context.Canceled)
	late := logs.FilterMessage("an MCP tool call's answer came after its request had ended").All()
	require.Len(t, late, 1)
	assert.Equal(t, "context canceled", late[0].ContextMap()["caller_gone"])

	listed := &mcpAsked{arrived: time.Now(), logger: zap.New(core).Sugar()}
	before := logs.Len()
	listed.ends(&writtenBack{ResponseWriter: httptest.NewRecorder()})
	assert.Equal(t, before, logs.Len())
}

// 6SION: an answer the MCP server cannot marshal was answered to nobody, and
// the caller waited until it gave up. It is a tool error the caller reads.
func TestAnAnswerThatDoesNotMarshalIsRefused(t *testing.T) {
	core, _ := observer.New(zapcore.InfoLevel)
	asked := &mcpAsked{arrived: time.Now(), logger: zap.New(core).Sugar()}
	handler := asked.recording("github_ask", func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"payload":"committer GitHub \spike"}`)}, nil
	})

	result, err := handler(context.Background(), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{}})
	require.NoError(t, err)
	asked.ends(&writtenBack{ResponseWriter: httptest.NewRecorder()})
	require.True(t, result.IsError)
	assert.Contains(t, textOf(t, result), "did not marshal")
}
