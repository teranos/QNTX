package server

// An MCP tool call leaves a record on the node.
//
// "as part of 6SION, we want to have more insights and observability into what happened to the session that was working on it"
//
// The access log says a request only when it answered 400 or more, after it
// answered. A tool call that fails is still a 200, and one that is never
// answered says nothing at all: 20 calls of 6SION left no line.

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teranos/QNTX/internal/measure"
	"go.uber.org/zap"
)

// What a call is in, in the order it passes them.
const (
	// The sigil, or the served route, is being asked.
	mcpAsking = "asking"
	// What it answered is with the scanner (fixtures.go).
	mcpMakingFixtures = "making_fixtures"
	// The same answer as structured content is with the scanner.
	mcpMakingStructuredFixtures = "making_structured_fixtures"
	// The answer is the MCP server's to write to whoever called.
	mcpWritingBack = "writing_back"
)

// mcpUnansweredAfter is how long a call is in flight before the node says it is
// not answered yet, and what it is in. GitHubService gives GitHub 30 seconds.
const mcpUnansweredAfter = 30 * time.Second

// mcpAsked is one request on the MCP endpoint and the tool call it carried, if
// it carried one.
type mcpAsked struct {
	arrived time.Time
	// "sentry is a thing you know"
	//
	// "but we dont do sentry well"
	//
	// The node's own log, which ships to Sentry as it is written.
	logger *zap.SugaredLogger

	mu   sync.Mutex
	tool string
	// "and then there is also OTEL"
	//
	// Anthropic sends a W3C traceparent in a tools/call's _meta that names its
	// own span of the call: with it the node's record and theirs are one call.
	traceparent string
	// What the sigil says of its own call: github ask's operation.
	said []any
	// What it is in now, since when, and how long each one before it took.
	in    string
	since time.Time
	took  []any
	// The handler gave its answer: whether it is a tool error, and its size.
	handed  bool
	isError bool
	chars   int
	// Why the caller's context ended before the handler was done, if it did.
	gone string
	// The line is written.
	ended      bool
	unanswered *time.Timer
}

type mcpAskedKey struct{}

// withMCPAsked is ctx carrying the record of the MCP request being answered.
func withMCPAsked(ctx context.Context, asked *mcpAsked) context.Context {
	return context.WithValue(ctx, mcpAskedKey{}, asked)
}

// mcpAskedOf is the record of the MCP request ctx is answered under, or nil:
// every method of a record is asked of nil too, and does nothing.
func mcpAskedOf(ctx context.Context) *mcpAsked {
	asked, _ := ctx.Value(mcpAskedKey{}).(*mcpAsked)
	return asked
}

// recording is handler, with the call it answers written into the record.
func (a *mcpAsked) recording(tool string, handler mcp.ToolHandler) mcp.ToolHandler {
	if a == nil {
		return handler
	}
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		traceparent := ""
		if req != nil && req.Params != nil {
			traceparent, _ = req.Params.Meta["traceparent"].(string)
		}
		a.begins(tool, traceparent)
		result, err := handler(withMCPAsked(ctx, a), req)
		a.hands(result, ctx.Err())
		return result, err
	}
}

// begins is the call arriving at its tool.
func (a *mcpAsked) begins(tool, traceparent string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tool, a.traceparent = tool, traceparent
	a.in, a.since = mcpAsking, time.Now()
	a.unanswered = time.AfterFunc(mcpUnansweredAfter, a.notYet)
}

// now is the call passing from what it was in to the next.
func (a *mcpAsked) now(in string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passes(in)
}

// passes closes what the call was in and opens the next. The lock is held.
func (a *mcpAsked) passes(in string) {
	at := time.Now()
	if a.in != "" {
		a.took = append(a.took, a.in+"_ms", at.Sub(a.since).Milliseconds())
	}
	a.in, a.since = in, at
}

// says is something the sigil says of its own call, for the line.
func (a *mcpAsked) says(key, value string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.said = append(a.said, key, value)
}

// hands is the handler giving its answer to the MCP server to write.
func (a *mcpAsked) hands(result *mcp.CallToolResult, gone error) {
	a.mu.Lock()
	a.passes(mcpWritingBack)
	a.handed = true
	if result != nil {
		a.isError = result.IsError
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				a.chars += len(text.Text)
			}
		}
	}
	if gone != nil {
		a.gone = gone.Error()
	}
	late, fields := a.ended, a.fields()
	a.mu.Unlock()
	// The request ended before the handler did: its line is written, and this
	// is what came after.
	if late {
		a.logger.Warnw("an MCP tool call's answer came after its request had ended", fields...)
	}
}

// notYet says a call still in flight, and what it is in.
func (a *mcpAsked) notYet() {
	a.mu.Lock()
	ended, fields := a.ended, a.fields()
	a.mu.Unlock()
	if ended {
		return
	}
	a.logger.Warnw("an MCP tool call is not answered yet", fields...)
}

// fields is the call as the line says it. The lock is held.
func (a *mcpAsked) fields() []any {
	fields := []any{"tool", a.tool}
	fields = append(fields, a.said...)
	fields = append(fields, a.took...)
	fields = append(fields,
		"in", a.in,
		"in_ms", time.Since(a.since).Milliseconds(),
		"waited_ms", time.Since(a.arrived).Milliseconds(),
		"handed", a.handed,
		"is_error", a.isError,
		"answer_chars", a.chars,
	)
	if a.gone != "" {
		fields = append(fields, "caller_gone", a.gone)
	}
	if a.traceparent != "" {
		fields = append(fields, "traceparent", a.traceparent)
	}
	return fields
}

// ends is the request answered, or given up on: the one line of its call. A
// request that carried no tool call (initialize, tools/list) says nothing.
func (a *mcpAsked) ends(wrote *writtenBack) {
	a.mu.Lock()
	if a.unanswered != nil {
		a.unanswered.Stop()
	}
	a.ended = true
	tool, handed, isError := a.tool, a.handed, a.isError
	fields := a.fields()
	a.mu.Unlock()
	if tool == "" {
		return
	}
	written, failed := wrote.wrote()
	fields = append(fields, "written_bytes", written)
	if failed != nil {
		fields = append(fields, "write_error", failed.Error())
	}

	outcome := "answered"
	switch {
	case !handed:
		outcome = "unanswered"
	case failed != nil || written == 0:
		outcome = "unwritten"
	case isError:
		outcome = "refused"
	}
	measure.Took(measure.MCPToolTook, time.Since(a.arrived),
		measure.String(measure.AttrTool, tool), measure.String(measure.AttrOutcome, outcome))

	switch outcome {
	case "unanswered":
		a.logger.Warnw("an MCP tool call's request ended before it was answered", fields...)
	case "unwritten":
		a.logger.Warnw("an MCP tool call was answered, and the answer was not written back", fields...)
	default:
		a.logger.Infow("an MCP tool call was answered", fields...)
	}
}

// writtenBack remembers what the MCP server wrote to whoever called: how much,
// and the first write that failed.
type writtenBack struct {
	http.ResponseWriter

	mu     sync.Mutex
	bytes  int
	failed error
}

func (w *writtenBack) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.mu.Lock()
	w.bytes += n
	if err != nil && w.failed == nil {
		w.failed = err
	}
	w.mu.Unlock()
	return n, err
}

// Flush passes through: the MCP server streams its answer.
func (w *writtenBack) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap is the writer underneath, for http.ResponseController.
func (w *writtenBack) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *writtenBack) wrote() (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytes, w.failed
}
