package server

// What the node does, as MCP tools (ADR-038).
//
// "a new thing is a new handler is a new mcp tool is a new api endpoint".
// "no handrolled tools".
//
// A sigil is the one place something QNTX does is defined (ADR-039), and a
// tool is one sigil: MCP is a surface of it, asked through the gate every
// route is behind and answered by the sigil itself (signa.go).
//
// "WHY DERIVE FROM SOMETHING THAT IS DERIVED IN THE FIRST PLACE"
//
// Every path no sigil answers yet still gets its tools the old way: read off
// the document at /openapi.json, which is itself generated from the Go source,
// one tool per method per mux line. Such a tool call is a request on the
// served mux carrying the caller's own credential, so it meets the gate its
// path's line sets. That goes as the signa are filled in.
//
// A caller is shown only the tools they reach.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/openapi"
	"github.com/teranos/errors"
)

// operation is one path and method the served document names.
type operation struct {
	Path        string
	Method      string
	Description string
	// Prefix is a route Go's mux matches by prefix: /api/types/ answers
	// /api/types/anything.
	Prefix bool
}

// operations is every operation a tool call can make: the document's, less
// the sockets, which a call cannot hold open, and the MCP endpoint itself.
var operations = sync.OnceValues(func() ([]operation, error) {
	var document struct {
		Paths map[string]map[string]struct {
			Description string `json:"description"`
			Socket      bool   `json:"x-qntx-websocket"`
			Prefix      bool   `json:"x-qntx-prefix"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openapi.Document(), &document); err != nil {
		return nil, errors.Wrap(err, "the generated OpenAPI document did not parse, so there are no tools")
	}
	var ops []operation
	for path, methods := range document.Paths {
		if path == "/mcp" || path == "/mcp/" {
			continue
		}
		for method, op := range methods {
			if op.Socket {
				continue
			}
			ops = append(ops, operation{
				Path:        path,
				Method:      strings.ToUpper(method),
				Description: op.Description,
				Prefix:      op.Prefix,
			})
		}
	}
	sort.Slice(ops, func(i, j int) bool { return toolName(ops[i]) < toolName(ops[j]) })
	return ops, nil
})

// toolName is the method and the path, in the characters a tool name allows:
// POST /api/attestations is post_api_attestations.
func toolName(op operation) string {
	name := []byte(strings.ToLower(op.Method))
	for i := 0; i < len(op.Path); i++ {
		c := op.Path[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			name = append(name, c)
			continue
		}
		name = append(name, '_')
	}
	return string(name)
}

// calledThrough is what every tool takes until a sigil states its own shape
// (ADR-039): which path, the query, and the JSON body.
type calledThrough struct {
	Path  string            `json:"path,omitempty"`
	Query map[string]string `json:"query,omitempty"`
	Body  json.RawMessage   `json:"body,omitempty"`
}

var calledThroughSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path": map[string]any{
			"type":        "string",
			"description": "The path to call. Defaults to the tool's route. A route ending in / or naming {a segment} answers more than one path; name the one meant here.",
		},
		"query": map[string]any{
			"type":                 "object",
			"additionalProperties": map[string]any{"type": "string"},
			"description":          "Query parameters.",
		},
		"body": map[string]any{
			"description": "The JSON body, for a method that takes one.",
		},
	},
}

// HandleMCP answers an MCP client with the API as tools.
//
// Stateless: a call is served under the request that carried it, so it is
// asked of the API with that request's credential and nobody else's.
func (s *QNTXServer) HandleMCP(w http.ResponseWriter, r *http.Request) {
	s.mcpOnce.Do(func() {
		s.mcpHTTP = mcp.NewStreamableHTTPHandler(s.mcpServerFor, &mcp.StreamableHTTPOptions{
			Stateless: true,
			// The node listens on loopback behind the proxy, so every request
			// arrives on 127.0.0.1 naming the public host, which the SDK refuses
			// as DNS rebinding. The gate in front has already admitted the caller.
			DisableLocalhostProtection: true,
		})
	})
	s.mcpHTTP.ServeHTTP(w, r)
}

// mcpServerFor is the server one request is answered by: one tool per
// operation, each calling through with this request's credential.
func (s *QNTXServer) mcpServerFor(r *http.Request) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "qntx", Version: version.VersionTag}, nil)
	ops, err := operations()
	if err != nil {
		if s.logger != nil {
			s.logger.Errorw("MCP offers no tools", "error", err)
		}
		return server
	}
	// A sigil is one tool, read from the sigil itself (ADR-039). The paths
	// sigils answer are left out of the document's tools below, so one thing is
	// offered once.
	//
	// A caller is shown only what they reach. Who is asking was settled by the
	// gate in front of /mcp and is in the request's context.
	admitted, known := auth.AdmissionFrom(r.Context())
	sigilled := map[string]bool{}
	for _, signum := range s.checkedSigna() {
		for _, sigil := range signum.GetSigils() {
			held := heldBy{signum: signum.GetName(), sigil: sigil, answer: signum.Answers[sigil.GetName()]}
			sigilled[sigil.GetHttp().GetPath()] = true
			if reaching, anyone := s.reachingOverMCP(held.signum, held.sigil); !offeredTo(admitted, known, reaching, anyone) {
				continue
			}
			server.AddTool(&mcp.Tool{
				Name:        toolNameOf(held.signum, held.sigil),
				Description: sigil.GetDoes(),
				InputSchema: takenAsSchema(held.sigil),
			}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := map[string]any{}
				if len(req.Params.Arguments) > 0 {
					if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
						return refused("the arguments to %s did not read: %v", held.sigil.GetName(), err), nil
					}
				}
				if s.served == nil {
					return refused("the node is not serving, so %s cannot be asked", held.sigil.GetName()), nil
				}
				// Asked again on the call, so a line written since the list was
				// drawn holds, and a tool that was listed is still gated.
				reaching, anyone := s.reachingOverMCP(held.signum, held.sigil)
				return askSigil(ctx, s.gate, reaching, anyone, r, held, args), nil
			})
		}
	}

	for _, op := range ops {
		if sigilled[op.Path] {
			continue
		}
		// The same cut for a tool the document still makes: who reaches its path.
		if s.served != nil {
			if reaching, anyone := s.served.Reaching(op.Path); !offeredTo(admitted, known, reaching, anyone) {
				continue
			}
		}
		server.AddTool(&mcp.Tool{
			Name:        toolName(op),
			Description: describe(op),
			InputSchema: calledThroughSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var in calledThrough
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
					return refused("the arguments to %s did not read: %v", toolName(op), err), nil
				}
			}
			if s.served == nil {
				return refused("the node is not serving, so %s %s cannot be asked", op.Method, op.Path), nil
			}
			return callThrough(ctx, s.served, r, op, in), nil
		})
	}
	return server
}

// describe is the handler's own prose and the route it answers.
func describe(op operation) string {
	said := strings.TrimSpace(op.Description)
	route := op.Method + " " + op.Path
	if op.Prefix {
		route += " (and every path under it)"
	}
	if said == "" {
		return route
	}
	return said + "\n\n" + route
}

// callThrough asks the served API what the tool was asked, as the caller.
func callThrough(ctx context.Context, served http.Handler, asked *http.Request, op operation, in calledThrough) *mcp.CallToolResult {
	path := op.Path
	if in.Path != "" {
		path = in.Path
	}
	if !answersOn(op, path) {
		return refused("%s is not a path %s %s answers", path, op.Method, op.Path)
	}

	target := path
	if len(in.Query) > 0 {
		query := url.Values{}
		for key, value := range in.Query {
			query.Set(key, value)
		}
		target += "?" + query.Encode()
	}
	var body *bytes.Reader
	if len(in.Body) > 0 && string(in.Body) != "null" {
		body = bytes.NewReader(in.Body)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, op.Method, target, body)
	if err != nil {
		return refused("%s %s could not be asked: %v", op.Method, target, err)
	}
	req.Host = asked.Host
	req.RemoteAddr = asked.RemoteAddr
	if len(in.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, carried := range []string{"Authorization", "X-Forwarded-For"} {
		if value := asked.Header.Get(carried); value != "" {
			req.Header.Set(carried, value)
		}
	}

	answer := &toolAnswer{header: http.Header{}}
	served.ServeHTTP(answer, req)

	status := answer.status
	if status == 0 {
		status = http.StatusOK
	}
	said := answer.body.String()
	if status >= http.StatusBadRequest {
		return refused("%s %s answered %d: %s", op.Method, target, status, strings.TrimSpace(said))
	}
	if said == "" {
		said = fmt.Sprintf("%s %s answered %d", op.Method, target, status)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: said}}}
}

// answersOn reports whether a path is one this operation's route answers:
// itself, anything under a prefix route, and anything at all for a route that
// names a segment, which the mux settles.
func answersOn(op operation, path string) bool {
	switch {
	case strings.Contains(op.Path, "{"):
		return true
	case op.Prefix:
		return strings.HasPrefix(path, op.Path)
	default:
		return path == op.Path
	}
}

// refused is a tool result that says what went wrong, in words.
func refused(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

// toolAnswer holds what the served API wrote, so it can be handed back as a
// tool result rather than written to the MCP client's connection.
type toolAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (a *toolAnswer) Header() http.Header { return a.header }

func (a *toolAnswer) WriteHeader(status int) {
	if a.status == 0 {
		a.status = status
	}
}

func (a *toolAnswer) Write(b []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	return a.body.Write(b)
}
