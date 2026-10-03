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
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// What the node serves from sigils (ADR-039). A signum that is filled in gives
// its paths to the mux, its sigils to MCP as tools and its operations to the
// served document, all three read from the same values.

// signa is every signum the node holds: its own, and every ready plugin's.
func (s *QNTXServer) signa() []sigil.Signum {
	return append([]sigil.Signum{s.staandsSignum(), s.iSignum(), s.reachSignum(), s.mailSignum(), s.pluginsSignum(), s.githubSignum(), s.namespacesSignum(), s.rolesSignum(), s.amSignum(), s.timeseriesSignum(), s.openapiSignum(), s.paritySignum(), s.transcriptsSignum()}, s.pluginSigna()...)
}

// checkedSigna is the signa that say what they hold. One that does not is said
// in the log and served nowhere: a sigil that defines half a thing is not
// something to offer.
func (s *QNTXServer) checkedSigna() []sigil.Signum {
	var kept []sigil.Signum
	for _, signum := range s.signa() {
		if err := signum.Check(); err != nil {
			if s.logger != nil {
				s.logger.Errorw("a signum is not served", "signum", signum.GetName(), "error", err)
			}
			continue
		}
		kept = append(kept, signum)
	}
	return kept
}

// toolNameOf is a sigil's name as a tool and in the document: the signum, then
// the sigil.
func toolNameOf(signum string, held *protocol.Sigil) string {
	return signum + "_" + held.GetName()
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
		if signum.Declared {
			continue
		}
		for _, held := range signum.GetSigils() {
			path := held.GetHttp().GetPath()
			bound[path] = append(bound[path], heldBy{signum: signum.GetName(), sigil: held, answer: signum.Answers[held.GetName()]})
		}
	}

	answering := map[string]http.HandlerFunc{}
	for path, sigils := range bound {
		answering[path] = overHTTP(path, sigils, s.gate, s.reachingOver, s.logger)
	}
	return answering
}

// heldBy is a sigil with the signum that holds it, which is what a reach line
// names it by, and the function that answers it.
type heldBy struct {
	signum string
	sigil  *protocol.Sigil
	answer sigil.Answer
}

// overHTTP is what answers on one path sigils are bound to. The method picks
// the sigil, and the sigil is asked: the gate is inside the asking, with every
// line about that sigil over HTTP (sigil.Asking).
//
// The mux does not gate such a path itself (reach.Answering.Gates), because
// its own line is only one of the lines about each sigil there. So nothing on
// the path is answered from outside the gate: a method no sigil is bound to is
// refused behind it too, with the row that admits ROOT alone, and whoever is
// not admitted learns nothing about what the path answers.
func overHTTP(path string, bound []heldBy, gate sigil.Gate, reaching func(string, heldBy) (auth.Reach, bool), logger *zap.SugaredLogger) http.HandlerFunc {
	var methods []string
	for _, held := range bound {
		methods = append(methods, held.sigil.GetHttp().GetMethod())
	}
	sort.Strings(methods)

	return func(w http.ResponseWriter, r *http.Request) {
		for _, held := range bound {
			if held.sigil.GetHttp().GetMethod() != r.Method {
				continue
			}
			arrived, err := arrivedIn(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			// A param the path names, {name}, arrives in the path.
			for _, param := range held.sigil.GetTakes() {
				if value := r.PathValue(param.GetName()); value != "" {
					arrived[param.GetName()] = value
				}
			}
			asked := held.asking(reach.OverHTTP, gate, reaching, r).Ask(r.Context(), arrived)
			switch {
			case asked.Rejected != nil:
				// The gate's answer, as it is: a 401 carries where to get a token.
				for name, values := range asked.Rejected.Header {
					w.Header()[name] = values
				}
				w.WriteHeader(asked.Rejected.Status)
				if _, err := w.Write([]byte(asked.Rejected.Body)); err != nil && logger != nil {
					logger.Errorw("could not write what the gate said", "route", path, "error", err)
				}
			case asked.Refusal != nil:
				writeError(w, sigil.Status(asked.Refusal), asked.Refusal.GetSays())
			default:
				respond(w, logger, http.StatusOK, asked.Answer)
			}
			return
		}
		gate(path, auth.Reach{}, func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusMethodNotAllowed,
				path+" answers "+strings.Join(methods, ", ")+", and this was "+r.Method)
		})(w, r)
	}
}

// asking is this sigil about to be asked over one surface, by one caller.
func (held heldBy) asking(surface string, gate sigil.Gate, reaching func(string, heldBy) (auth.Reach, bool), caller *http.Request) sigil.Asking {
	allowed, anyone := reaching(surface, held)
	return sigil.Asking{
		Surface: surface, Signum: held.signum, Sigil: held.sigil, Answer: held.answer,
		Gate: gate, Reaching: allowed, Anyone: anyone, Caller: caller,
	}
}

// reachingOver is who reaches one sigil over one surface: every line about
// it, by its path and by name (reach.ReachingSigil). Before anything is open
// that is nobody beside ROOT.
func (s *QNTXServer) reachingOver(surface string, held heldBy) (auth.Reach, bool) {
	if s.served == nil {
		return auth.Reach{}, false
	}
	return s.served.ReachingSigil(surface, held.signum, held.sigil.GetName(), held.sigil.GetHttp().GetPath())
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
func schemaTypeOf(kind string) string {
	if kind == sigil.Count {
		return "integer"
	}
	return "string"
}

// takenAsSchema is what a sigil takes, in the form a tool says it: a JSON
// Schema object, a property per param.
func takenAsSchema(held *protocol.Sigil) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, param := range held.GetTakes() {
		property := map[string]any{"type": schemaTypeOf(param.GetKind()), "description": param.GetSays()}
		if len(param.GetOneOf()) > 0 {
			property["enum"] = param.GetOneOf()
		}
		properties[param.GetName()] = property
		if param.GetRequired() {
			required = append(required, param.GetName())
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

// givenAsSchema is what a sigil gives, as MCP's outputSchema: one object, or a
// list of them a row at a time, with the fields it names. Nil when it names
// none.
//
// A Field says nothing of whether it is always there, and an answer leaves
// some out (plugins list's health_probed_at is "absent before the first
// probe") and one carries a field it does not name, so no field is required
// and none is refused.
func givenAsSchema(held *protocol.Sigil) map[string]any {
	if len(held.GetGives()) == 0 {
		return nil
	}
	properties := map[string]any{}
	for _, field := range held.GetGives() {
		properties[field.GetName()] = map[string]any{"description": field.GetSays()}
	}
	row := map[string]any{"type": "object", "properties": properties}
	return map[string]any{"anyOf": []any{row, map[string]any{"type": "array", "items": row}, map[string]any{"type": "null"}}}
}

// shapedAsGiven is whether an answer is what givenAsSchema promises: one
// object, a list of objects, or nothing.
func shapedAsGiven(body []byte) error {
	var answer any
	if err := json.Unmarshal(body, &answer); err != nil {
		return err
	}
	switch answer := answer.(type) {
	case nil, map[string]any:
		return nil
	case []any:
		for i, row := range answer {
			if _, ok := row.(map[string]any); !ok {
				return errors.Newf("row %d of the answer is %T, not an object", i, row)
			}
		}
		return nil
	default:
		return errors.Newf("the answer is %T, neither an object nor a list of them", answer)
	}
}

// annotationsOf is what a sigil's method promises, as MCP's hints: a GET is
// safe, and a PUT and a DELETE are idempotent (RFC 9110 §9.2). Any other
// method promises neither, and the spec's defaults stand.
func annotationsOf(held *protocol.Sigil) *mcp.ToolAnnotations {
	switch held.GetHttp().GetMethod() {
	case http.MethodGet:
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	case http.MethodPut, http.MethodDelete:
		return &mcp.ToolAnnotations{IdempotentHint: true}
	}
	return nil
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

// overMCP is a tool call on a sigil. MCP is a surface of the sigil and not a
// caller of its endpoint: the arguments are what arrived, the sigil is asked
// with the gate inside the asking and the credential the MCP request carried,
// and the answer is the sigil's own, never an HTTP response read back.
//
// The answer is the result's structured content, and its text too. A tool that
// says what it gives must give that (outputSchema), so its answer is held to
// the shape it promised first.
func overMCP(ctx context.Context, gate sigil.Gate, reaching func(string, heldBy) (auth.Reach, bool), caller *http.Request, held heldBy, args map[string]any, saysWhatItGives bool) *mcp.CallToolResult {
	asked := held.asking(reach.OverMCP, gate, reaching, caller).Ask(ctx, args)
	switch {
	case asked.Rejected != nil:
		// The gate's answer, in the gate's own words.
		return refused("%s is not yours to ask: %s", held.sigil.GetName(), strings.TrimSpace(asked.Rejected.Body))
	case asked.Refusal != nil:
		return refused("%s", refusalSays(asked.Refusal))
	}
	body, err := json.Marshal(asked.Answer)
	if err != nil {
		return refused("what %s answered does not marshal: %v", held.sigil.GetName(), err)
	}
	if saysWhatItGives {
		if err := shapedAsGiven(body); err != nil {
			return refused("%s: what %s answered is not what it says it gives: %v", sigil.Failed, held.sigil.GetName(), err)
		}
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(body)}},
		StructuredContent: json.RawMessage(body),
	}
}

// refusalSays is a refusal in its own terms: the kind of no, the param the
// caller has to change when it names one, and what it says. A tool error has
// only words, so they carry what the HTTP API gives as a status.
func refusalSays(r *protocol.Refusal) string {
	why := r.GetWhy()
	if r.GetParam() != "" {
		why += " (" + r.GetParam() + ")"
	}
	if why == "" {
		return r.GetSays()
	}
	return why + ": " + r.GetSays()
}

// sigilsInto lays the sigils' operations into the written document, whose
// paths are the reach table's and say who reaches them and nothing else.
func sigilsInto(document map[string]any, signa []sigil.Signum) {
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		return
	}
	for _, signum := range signa {
		for _, held := range signum.GetSigils() {
			path := held.GetHttp().GetPath()
			operations, ok := paths[path].(map[string]any)
			if !ok {
				// A path the table does not name is not served, so the document
				// does not say it either.
				continue
			}

			op := map[string]any{
				"summary":      held.GetDoes(),
				"x-qntx-reach": operations["x-qntx-reach"],
				"x-qntx-sigil": toolNameOf(signum.GetName(), held),
			}
			if carriesBody(held.GetHttp().GetMethod()) {
				op["requestBody"] = map[string]any{"content": map[string]any{
					"application/json": map[string]any{"schema": takenAsSchema(held)},
				}}
			} else {
				parameters := []any{}
				for _, param := range held.GetTakes() {
					schema := map[string]any{"type": schemaTypeOf(param.GetKind())}
					if len(param.GetOneOf()) > 0 {
						schema["enum"] = param.GetOneOf()
					}
					in := "query"
					if strings.Contains(path, "{"+param.GetName()+"}") {
						in = "path"
					}
					parameters = append(parameters, map[string]any{
						"name": param.GetName(), "in": in, "required": param.GetRequired(),
						"description": param.GetSays(), "schema": schema,
					})
				}
				op["parameters"] = parameters
			}
			operations[strings.ToLower(held.GetHttp().GetMethod())] = op
		}
	}
}
