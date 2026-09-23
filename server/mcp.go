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
// Every path no sigil answers yet is a tool too, read off what the node serves
// (reach.Served.Routes): no document, no source.

// Nothing there says which methods a path answers or what it is for, so the
// caller names the method and the tool says only its route.

// Such a call is a request on the served mux with the caller's own credential,
// so it meets the gate its path's line sets. A caller is shown only what they reach.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

// operation is one route no sigil answers yet, called with one method.
type operation struct {
	Path   string
	Method string
	// Prefix is a route Go's mux matches by prefix: /api/types/ answers
	// /api/types/anything.
	Prefix bool
}

// routeTool is whether a served route is a tool: not a socket, which a call
// cannot hold open, not a path sigils answer, and not the MCP endpoint itself.
func routeTool(route reach.Route) bool {
	return !route.Socket && !route.Gates && route.Path != "/mcp" && route.Path != "/mcp/"
}

// namespaced is a path that says or moves where its caller acts. To a
// connector the namespace does not exist (ADR-038), so none is offered to one.
func namespaced(admitted auth.Admission, path string) bool {
	if admitted.ClientDID == "" {
		return false
	}
	return path == "/i/" || path == "/i/standing" || strings.HasPrefix(path, "/api/namespaces")
}

// toolName is the route in the characters a tool name allows, after the
// surface it is called over: /api/attestations is http_api_attestations.
func toolName(path string) string {
	name := []byte(reach.OverHTTP)
	for i := 0; i < len(path); i++ {
		c := path[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			name = append(name, c)
			continue
		}
		name = append(name, '_')
	}
	return string(name)
}

// calledThrough is what every tool takes until a sigil states its own shape
// (ADR-039): the method, which path, the query, and the JSON body.
type calledThrough struct {
	Method string            `json:"method"`
	Path   string            `json:"path,omitempty"`
	Query  map[string]string `json:"query,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
}

var calledThroughSchema = map[string]any{
	"type":     "object",
	"required": []string{"method"},
	"properties": map[string]any{
		"method": map[string]any{
			"type":        "string",
			"enum":        []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
			"description": "The HTTP method. No sigil answers this route yet, so nothing says which methods it answers.",
		},
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
	// A sigil is one tool, read from the sigil itself (ADR-039). Its path gates
	// itself and is no route tool below, so one thing is offered once.

	// Who is asking was settled by the gate in front of /mcp and is in the
	// request's context.
	admitted, known := auth.AdmissionFrom(r.Context())
	for _, signum := range s.checkedSigna() {
		for _, sigil := range signum.GetSigils() {
			held := heldBy{signum: signum.GetName(), sigil: sigil, answer: signum.Answers[sigil.GetName()]}
			if reaching, anyone := s.reachingOver(reach.OverMCP, held); !offeredTo(admitted, known, reaching, anyone) {
				continue
			}
			if namespaced(admitted, sigil.GetHttp().GetPath()) {
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
				// Who reaches it is asked again on the call, so a line written
				// since the list was drawn holds, and a listed tool is still gated.
				return overMCP(ctx, s.gate, s.reachingOver, r, held, args), nil
			})
		}
	}

	// Before anything is open the node serves nothing, and there is no route to
	// make a tool of.
	if s.served == nil {
		return server
	}
	for _, route := range s.served.Routes() {
		if !routeTool(route) {
			continue
		}
		// The same cut for a route's tool: who reaches its path.
		if reaching, anyone := s.served.Reaching(route.Path); !offeredTo(admitted, known, reaching, anyone) {
			continue
		}
		if namespaced(admitted, route.Path) {
			continue
		}
		path := route.Path
		prefix := strings.HasSuffix(path, "/") && path != "/"
		server.AddTool(&mcp.Tool{
			Name:        toolName(path),
			Description: describe(path, prefix),
			InputSchema: calledThroughSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var in calledThrough
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
					return refused("the arguments to %s did not read: %v", toolName(path), err), nil
				}
			}
			if in.Method == "" {
				return refused("%s needs a method: nothing says which methods %s answers", toolName(path), path), nil
			}
			op := operation{Path: path, Method: strings.ToUpper(in.Method), Prefix: prefix}
			return callThrough(ctx, s.served, r, op, in), nil
		})
	}
	return server
}

// describe is the route a tool calls. Nothing else is known of it until a
// sigil answers there.
func describe(path string, prefix bool) string {
	route := path
	if prefix {
		route += " (and every path under it)"
	}
	return "Calls " + route + " on this node's HTTP API. No sigil answers it yet, so what it takes and answers is not stated."
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
	// A route naming a segment answers any path, so the one asked is checked too.
	if admitted, _ := auth.AdmissionFrom(asked.Context()); namespaced(admitted, path) {
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
