package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// What the node serves from sigils (ADR-039). A signum that is filled in gives
// its paths to the mux, its sigils to MCP as tools and its operations to the
// served document, all three read from the same values.

// signa is every signum the node holds. One so far.
func (s *QNTXServer) signa() []sigil.Signum {
	return []sigil.Signum{s.staandsSignum()}
}

// checkedSigna is the signa that say what they hold. One that does not is said
// in the log and served nowhere: a sigil that defines half a thing is not
// something to offer.
func (s *QNTXServer) checkedSigna() []sigil.Signum {
	var kept []sigil.Signum
	for _, signum := range s.signa() {
		if err := signum.Check(); err != nil {
			if s.logger != nil {
				s.logger.Errorw("a signum is not served", "signum", signum.Name, "error", err)
			}
			continue
		}
		kept = append(kept, signum)
	}
	return kept
}

// toolNameOf is a sigil's name as a tool and in the document: the signum, then
// the sigil.
func toolNameOf(signum sigil.Signum, held sigil.Sigil) string {
	return signum.Name + "_" + held.Name
}

// carriesBody reports whether a method's params ride a JSON body. A GET and a
// DELETE carry theirs in the query, which is the HTTP API's rule for where a
// param travels (sigil.Param).
func carriesBody(method string) bool {
	return method != http.MethodGet && method != http.MethodDelete
}

// answeredFromSigils is one handler per path a sigil is bound to. The method
// picks the sigil, what was sent is read against what it takes, and only what
// the sigil does not refuse reaches the function that answers.
func (s *QNTXServer) answeredFromSigils() map[string]http.HandlerFunc {
	bound := map[string][]heldBy{}
	for _, signum := range s.checkedSigna() {
		for _, held := range signum.Sigils {
			bound[held.HTTP.Path] = append(bound[held.HTTP.Path], heldBy{signum: signum.Name, sigil: held})
		}
	}

	answering := map[string]http.HandlerFunc{}
	for path, sigils := range bound {
		answering[path] = overHTTP(path, sigils, s.gate, s.reachingOverHTTP, s.logger)
	}
	return answering
}

// heldBy is a sigil with the signum that holds it, which is what a reach line
// names it by.
type heldBy struct {
	signum string
	sigil  sigil.Sigil
}

// overHTTP is what answers on one path sigils are bound to. The method picks
// the sigil, and that sigil is put behind the gate with every line about it,
// by its path and by name. One path can hold sigils different people reach:
// listing the stands and taking one down are both /api/staands.
//
// The mux does not gate such a path itself (reach.Answering.Gates), because
// its own line is only one of the lines about each sigil there. So nothing on
// the path is answered from outside the gate: a method no sigil is bound to is
// refused behind it too, with the row that admits ROOT alone, and whoever is
// not admitted learns nothing about what the path answers.
func overHTTP(path string, bound []heldBy, gate reach.Gate, reaching func(heldBy) (auth.Reach, bool), logger *zap.SugaredLogger) http.HandlerFunc {
	var methods []string
	for _, held := range bound {
		methods = append(methods, held.sigil.HTTP.Method)
	}
	sort.Strings(methods)

	return func(w http.ResponseWriter, r *http.Request) {
		for _, held := range bound {
			if held.sigil.HTTP.Method != r.Method {
				continue
			}
			answers := func(w http.ResponseWriter, r *http.Request) {
				arrived, err := arrivedIn(r)
				if err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				answer, refusal := askedOf(r.Context(), held.sigil, arrived)
				if refusal != nil {
					writeError(w, refusal.Status(), refusal.Says)
					return
				}
				respond(w, logger, http.StatusOK, answer)
			}
			if allowed, anyone := reaching(held); !anyone {
				answers = gate(path, allowed, answers)
			}
			answers(w, r)
			return
		}
		gate(path, auth.Reach{}, func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusMethodNotAllowed,
				path+" answers "+strings.Join(methods, ", ")+", and this was "+r.Method)
		})(w, r)
	}
}

// reachingOverHTTP is who reaches one sigil over the HTTP API: every line
// about it, by its path and by name (reach.ReachingSigil). Before anything is
// open that is nobody beside ROOT.
func (s *QNTXServer) reachingOverHTTP(held heldBy) (auth.Reach, bool) {
	if s.served == nil {
		return auth.Reach{}, false
	}
	return s.served.ReachingSigil(reach.OverHTTP, held.signum, held.sigil.Name, held.sigil.HTTP.Path)
}

// arrivedIn is what a request carried, by name: its query, or its JSON body
// when the method carries one. The HTTP API's half of reading what arrives;
// what each value is read as is the sigil's (sigil.Read).
func arrivedIn(r *http.Request) (map[string]any, error) {
	arrived := map[string]any{}
	if !carriesBody(r.Method) {
		for name := range r.URL.Query() {
			arrived[name] = r.URL.Query().Get(name)
		}
		return arrived, nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "the body of %s %s did not read", r.Method, r.URL.Path)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return arrived, nil
	}
	if err := json.Unmarshal(raw, &arrived); err != nil {
		return nil, errors.Newf("the body of %s %s is not a JSON object", r.Method, r.URL.Path)
	}
	return arrived, nil
}

// schemaTypeOf is a kind in JSON Schema's words, which a tool and the document
// both speak.
func schemaTypeOf(kind sigil.Kind) string {
	if kind == sigil.Count {
		return "integer"
	}
	return "string"
}

// takenAsSchema is what a sigil takes, in the form a tool says it: a JSON
// Schema object, a property per param.
func takenAsSchema(held sigil.Sigil) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, param := range held.Takes {
		property := map[string]any{"type": schemaTypeOf(param.Kind), "description": param.Says}
		if len(param.OneOf) > 0 {
			property["enum"] = param.OneOf
		}
		properties[param.Name] = property
		if param.Required {
			required = append(required, param.Name)
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

// offeredTo reports whether a tool is shown to whoever is asking: a caller is
// shown only what they reach. It asks the gate's own question of the row the
// gate will be given on the call, so the list and the gate cannot disagree. A
// node with no login knows nobody, and shows every tool as it serves every
// route.
func offeredTo(admitted auth.Admission, known bool, reaching auth.Reach, anyone bool) bool {
	if !known || anyone {
		return true
	}
	return reaching.Admits(admitted)
}

// reachingOverMCP is who reaches one sigil over MCP: every line about it, by
// its path and by name (reach.ReachingSigil). Asked on every list and every
// call, so a line written at runtime holds from the next one.
func (s *QNTXServer) reachingOverMCP(signum sigil.Signum, held sigil.Sigil) (auth.Reach, bool) {
	if s.served == nil {
		return auth.Reach{}, false
	}
	return s.served.ReachingSigil(reach.OverMCP, signum.Name, held.Name, held.HTTP.Path)
}

// askedOf is one sigil asked, whichever surface carried the asking: what was
// sent is read against what the sigil takes, and only what it does not refuse
// reaches the function that answers. Every surface refuses the same way
// because every surface refuses here.
func askedOf(ctx context.Context, held sigil.Sigil, arrived map[string]any) (any, *sigil.Refusal) {
	sent, refusal := held.Read(arrived)
	if refusal != nil {
		return nil, refusal
	}
	if refusal := held.Refuses(sent); refusal != nil {
		return nil, refusal
	}
	return held.Answer(ctx, sent)
}

// askSigil is a tool call on a sigil. MCP is a surface of the sigil and not a
// caller of its endpoint: the arguments are what was sent, and the answer is
// the sigil's own, never an HTTP response read back.
//
// It is put behind the gate every route is behind, with the row the lines give
// the sigil and the credential the MCP request carried, so who may ask is
// decided where it always is and nowhere else. The gate hands on the context
// with who was admitted in it, which is what the function that answers reads.
func askSigil(ctx context.Context, gate reach.Gate, reaching auth.Reach, anyone bool, caller *http.Request, held sigil.Sigil, args map[string]any) *mcp.CallToolResult {
	var result *mcp.CallToolResult
	admitted := func(_ http.ResponseWriter, r *http.Request) {
		answer, refusal := askedOf(r.Context(), held, args)
		if refusal != nil {
			result = refused("%s", refusal.Says)
			return
		}
		body, err := json.Marshal(answer)
		if err != nil {
			result = refused("what %s answered does not marshal: %v", held.Name, err)
			return
		}
		result = &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}
	}
	if !anyone {
		admitted = gate(held.HTTP.Path, reaching, admitted)
	}

	// The gate reads a credential off a request, so it is given one: the
	// sigil's own endpoint, carrying what the MCP request carried.
	asking, err := http.NewRequestWithContext(ctx, held.HTTP.Method, held.HTTP.Path, nil)
	if err != nil {
		return refused("%s could not be asked: %v", held.Name, err)
	}
	asking.Host = caller.Host
	asking.RemoteAddr = caller.RemoteAddr
	for _, carried := range []string{"Authorization", "Cookie", "X-Forwarded-For"} {
		if value := caller.Header.Get(carried); value != "" {
			asking.Header.Set(carried, value)
		}
	}
	turnedAway := &toolAnswer{header: http.Header{}}
	admitted(turnedAway, asking)

	if result == nil {
		// The gate answered and the sigil never did: whoever asked does not
		// reach it, in the gate's own words.
		return refused("%s is not yours to ask: %s", held.Name, strings.TrimSpace(turnedAway.body.String()))
	}
	return result
}

// sigilsInto lays the sigils over the generated document, a path at a time.
// The generator reads source and a route offered from a sigil is not a literal
// there, so the written file cannot say what a sigil does. Who reaches a path
// is the table's to say and is kept.
func sigilsInto(document map[string]any, signa []sigil.Signum) {
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		return
	}
	// Who reaches each path, read before anything is laid over it. Reach is per
	// path in the table, so any operation the generator wrote there carries it.
	reached := map[string]any{}
	for _, signum := range signa {
		for _, held := range signum.Sigils {
			// A path the generator did not write is not in the document, and
			// ranging over nothing is what that means.
			generated, written := paths[held.HTTP.Path].(map[string]any)
			if !written {
				continue
			}
			for _, op := range generated {
				if written, ok := op.(map[string]any); ok && written["x-qntx-reach"] != nil {
					reached[held.HTTP.Path] = written["x-qntx-reach"]
				}
			}
		}
	}
	for path := range reached {
		paths[path] = map[string]any{}
	}

	for _, signum := range signa {
		for _, held := range signum.Sigils {
			path := held.HTTP.Path
			operations, ok := paths[path].(map[string]any)
			if !ok {
				// A path the table does not name is not served, so the document
				// does not say it either.
				continue
			}

			op := map[string]any{
				"summary":      held.Does,
				"x-qntx-reach": reached[path],
				"x-qntx-sigil": toolNameOf(signum, held),
			}
			if carriesBody(held.HTTP.Method) {
				op["requestBody"] = map[string]any{"content": map[string]any{
					"application/json": map[string]any{"schema": takenAsSchema(held)},
				}}
			} else {
				parameters := []any{}
				for _, param := range held.Takes {
					schema := map[string]any{"type": schemaTypeOf(param.Kind)}
					if len(param.OneOf) > 0 {
						schema["enum"] = param.OneOf
					}
					in := "query"
					if strings.Contains(path, "{"+param.Name+"}") {
						in = "path"
					}
					parameters = append(parameters, map[string]any{
						"name": param.Name, "in": in, "required": param.Required,
						"description": param.Says, "schema": schema,
					})
				}
				op["parameters"] = parameters
			}
			operations[strings.ToLower(held.HTTP.Method)] = op
		}
	}
}
