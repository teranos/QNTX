package a2a

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/server/parity"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// The HTTP+JSON binding (§11): one adapter onto the operations, which never
// see a request or a response.

// Version is the A2A version this node speaks, Major.Minor (§3.6).
const Version = "1.0"

// contentType is §11.1: application/a2a+json SHOULD be used.
const contentType = "application/a2a+json"

// Answer does one operation: what was asked, as its request message, and the
// response message or an error.
type Answer func(ctx context.Context, op Operation, request proto.Message) (proto.Message, *Error)

// Undelivered is told of a response that did not reach the caller, which leaves
// no trace anywhere else.
type Undelivered func(what string, err error)

// Refused is told of each request the binding answers with an error: what a
// client asked that the node does not answer, which leaves no trace anywhere
// else. "how would we know what things like chatGPT are trying to do and
// call? Things it might expect"
type Refused func(r *http.Request, refused *Error)

// Served is whether an agent is served at tenant (AgentInterface.tenant), or
// why that was not answered.
type Served func(tenant string) (bool, error)

// HTTP serves the operations on their routes, relative to wherever it is
// mounted. Every request is read the same way before any operation answers.
func HTTP(operations []Operation, served Served, answer Answer, undelivered Undelivered, told Refused) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuse := func(refused *Error) {
			told(r, refused)
			refused.write(w, undelivered)
		}
		op, params, found := match(operations, r.Method, r.URL.Path)
		// A tenant no agent is served at is a resource that does not exist, and
		// answers as no operation does: "MUST return a not found error when a
		// requested resource does not exist", and "SHOULD NOT distinguish
		// between 'does not exist' and 'not authorized'" (§3.3.2).
		if tenant, named := params["tenant"]; found && named {
			at, err := served(tenant)
			if err != nil {
				refuse(&Error{Message: "whether an agent is served at tenant " + tenant + " was not answered: " + err.Error()})
				return
			}
			found = at
		}
		if !found {
			refuse(notFound(r.Method + " " + r.URL.Path + " is no A2A operation" + jsonRPCAsked(r)))
			return
		}
		if refused := version(r); refused != nil {
			refuse(refused)
			return
		}
		request, refused := read(op, r, params)
		if refused != nil {
			refuse(refused)
			return
		}
		// A tenant can arrive in the query or the body as well as the path, and
		// is the same resource wherever it arrived.
		if tenant, named := TenantOf(request); named {
			at, err := served(tenant)
			if err != nil {
				refuse(&Error{Message: "whether an agent is served at tenant " + tenant + " was not answered: " + err.Error()})
				return
			}
			if !at {
				refuse(notFound("no agent is served at tenant " + tenant))
				return
			}
		}
		if refused := lacking(op, request); refused != nil {
			refuse(refused)
			return
		}
		response, refused := answer(r.Context(), op, request)
		if refused != nil {
			refuse(refused)
			return
		}
		if op.Streams {
			// A stream is Server-Sent Events (§11.7), and no operation here
			// streams yet.
			refuse(&Error{Name: "InvalidAgentResponseError", Message: op.Name + " answered without a stream"})
			return
		}
		body, err := marshalled(response)
		if err != nil {
			refuse(&Error{Name: "InvalidAgentResponseError", Message: err.Error()})
			return
		}
		if op.Name == getExtendedAgentCard && cached(w, r, body) {
			return
		}
		w.Header().Set("Content-Type", contentType)
		if _, err := w.Write(body); err != nil {
			undelivered(op.Name, err)
		}
	})
}

// getExtendedAgentCard is the operation whose card is the caller's own.
const getExtendedAgentCard = "GetExtendedAgentCard"

// extendedCardMaxAge is how long a client keeps an extended card, short as the
// public card's is: health is on the card.
const extendedCardMaxAge = "60"

// cached sets how a client caches an extended card, and answers 304 when the
// card it holds is the one it would be sent: "Agents SHOULD implement
// appropriate caching headers to control client-side caching of extended
// cards", and "honor client cache invalidation" (§13.3). The card is the
// caller's, what they reach, so it is private and kept by no shared cache.
func cached(w http.ResponseWriter, r *http.Request, body []byte) bool {
	// marshalled varies its whitespace as protojson does; compacted, the ETag
	// is the card's and not the encoder's.
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		return false
	}
	sum := sha256.Sum256(compact.Bytes())
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Cache-Control", "private, max-age="+extendedCardMaxAge)
	w.Header().Set("ETag", etag)
	if Matches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// Matches is If-None-Match against one strong ETag (RFC 9110 §13.1.2): a list
// of tags, a weak one compared by its opaque part, or *.
func Matches(header, etag string) bool {
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
		if tag == "*" || tag == etag {
			return true
		}
	}
	return false
}

// marshalled is response as JSON, each REQUIRED field of it present though
// it holds its default: "The nextPageToken field MUST always be present in the
// response" (§3.1.4), and protojson leaves out what holds its default.
func marshalled(response proto.Message) ([]byte, error) {
	body, err := protojson.Marshal(response)
	if err != nil {
		return nil, err
	}
	whole, err := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(response)
	if err != nil {
		return nil, err
	}
	var given, defaulted map[string]json.RawMessage
	if err := json.Unmarshal(body, &given); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(whole, &defaulted); err != nil {
		return nil, err
	}
	fields := response.ProtoReflect().Descriptor().Fields()
	added := false
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		value, held := defaulted[field.JSONName()]
		if _, given := given[field.JSONName()]; given || !held || !parity.Required(field) {
			continue
		}
		given[field.JSONName()], added = value, true
	}
	if !added {
		return body, nil
	}
	return json.Marshal(given)
}

// TenantOf is the tenant a request message names, when it names one: the
// routing identifier every request carries (AgentInterface.tenant, §8.3.2).
func TenantOf(request proto.Message) (string, bool) {
	tenant, named := "", false
	request.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Name() != "tenant" || field.Kind() != protoreflect.StringKind {
			return true
		}
		tenant, named = value.String(), true
		return false
	})
	return tenant, named
}

// version is §3.6.2: the A2A-Version header or request parameter, an empty
// one meaning 0.3, compared by Major.Minor alone.
func version(r *http.Request) *Error {
	said := r.Header.Get("A2A-Version")
	if said == "" {
		said = r.URL.Query().Get("A2A-Version")
	}
	if said == "" {
		said = "0.3"
	}
	parts := strings.Split(said, ".")
	if len(parts) >= 2 && parts[0]+"."+parts[1] == Version {
		return nil
	}
	return &Error{Name: "VersionNotSupportedError", Message: "A2A-Version " + said + " is not supported; this node speaks " + Version}
}

// lacking is §3.3.2 and §5.7: a request that does not set what the spec
// requires is a validation error, naming each field, before any operation is
// asked. It is read in the version §3.6.2 already settled.
func lacking(op Operation, request proto.Message) *Error {
	missing := Missing(request.ProtoReflect())
	if len(missing) == 0 {
		return nil
	}
	name := string(op.request.Name())
	fields := make([]string, 0, len(missing))
	for _, path := range missing {
		fields = append(fields, strings.TrimPrefix(path, name+"."))
	}
	refused := invalid(name + " lacks what the spec requires: " + strings.Join(fields, ", "))
	refused.Lacking = fields
	return refused
}

// match finds the operation a method and path name, and the path parameters,
// trying the routes without a tenant first.
func match(operations []Operation, method, path string) (Operation, map[string]string, bool) {
	for _, tenant := range []bool{false, true} {
		for _, op := range operations {
			route := op.HTTP
			if tenant {
				route = op.Tenant
			}
			if route.Method != method {
				continue
			}
			if params, ok := matchPath(route.Path, path); ok {
				return op, params, true
			}
		}
	}
	return Operation{}, nil, false
}

// matchPath matches a route's path, whose segments are literal, {name}, or
// {name} with a literal after it in the same segment: {id}:cancel, where
// :cancel is the route's verb.
func matchPath(template, path string) (map[string]string, bool) {
	want := strings.Split(strings.Trim(template, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return nil, false
	}
	params := map[string]string{}
	for i, segment := range want {
		if !strings.HasPrefix(segment, "{") {
			if segment != got[i] {
				return nil, false
			}
			continue
		}
		closing := strings.Index(segment, "}")
		if closing < 0 {
			return nil, false
		}
		name, after := segment[1:closing], segment[closing+1:]
		value, ok := strings.CutSuffix(got[i], after)
		if !ok || value == "" {
			return nil, false
		}
		// The last segment's :verb is the route's: /tasks/abc:subscribe is not
		// GET /tasks/{id} with an id of abc:subscribe.
		if i == len(want)-1 && after == "" && strings.Contains(value, ":") {
			return nil, false
		}
		params[name] = value
	}
	return params, true
}

// read is the operation's request message: the JSON body (§11.4) or, for a
// method without one, the query parameters by their camelCase names (§11.5),
// then the path parameters. Fields the request does not have are ignored
// (§5.7).
func read(op Operation, r *http.Request, params map[string]string) (proto.Message, *Error) {
	request := dynamicpb.NewMessage(op.request)
	unmarshal := protojson.UnmarshalOptions{DiscardUnknown: true}
	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		values, refused := queried(op.request, r)
		if refused != nil {
			return nil, refused
		}
		body, err := json.Marshal(values)
		if err != nil {
			return nil, invalid(err.Error())
		}
		if err := unmarshal.Unmarshal(body, request); err != nil {
			return nil, invalid("the query parameters are not a " + string(op.request.Name()) + ": " + err.Error())
		}
	default:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, invalid("the body did not read: " + err.Error())
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := unmarshal.Unmarshal(body, request); err != nil {
				return nil, invalid("the body is not a " + string(op.request.Name()) + ": " + err.Error())
			}
		}
	}
	for name, value := range params {
		field := op.request.Fields().ByName(protoreflect.Name(name))
		if field == nil || field.Kind() != protoreflect.StringKind {
			return nil, invalid(string(op.request.Name()) + " has no " + name + " to take from the path")
		}
		request.Set(field, protoreflect.ValueOfString(value))
	}
	return request, nil
}

// queried is the query parameters the request has fields for, as JSON values
// protojson reads: a list for a repeated field, true or false for a bool, and
// the string as written otherwise (§11.5). An enum's value is one of its
// names, which the unknown are not discarded as: "Invalid status value
// 'TASK_STATE_RUNNING'" is refused (§6.5).
func queried(message protoreflect.MessageDescriptor, r *http.Request) (map[string]any, *Error) {
	values := map[string]any{}
	for name, said := range r.URL.Query() {
		field := message.Fields().ByJSONName(name)
		if field == nil || len(said) == 0 {
			continue
		}
		switch {
		case field.IsList():
			var all []string
			for _, one := range said {
				all = append(all, strings.Split(one, ",")...)
			}
			values[name] = slices.DeleteFunc(all, func(v string) bool { return v == "" })
		case field.Kind() == protoreflect.EnumKind && field.Enum().Values().ByName(protoreflect.Name(said[0])) == nil:
			return nil, invalid("invalid " + name + " value '" + said[0] + "': it is no " + string(field.Enum().FullName()))
		case field.Kind() == protoreflect.BoolKind:
			if b, err := strconv.ParseBool(said[0]); err == nil {
				values[name] = b
			} else {
				values[name] = said[0]
			}
		default:
			values[name] = said[0]
		}
	}
	return values, nil
}

// jsonRPCAsked is what a JSON-RPC request (§9) asked, said after a path no
// operation answers: the node serves the HTTP+JSON binding (§11) alone, so a
// JSON-RPC client is told which method it called, and the node learns it.
// Empty for anything else.
func jsonRPCAsked(r *http.Request) string {
	if r.Body == nil || r.Method != http.MethodPost {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return ""
	}
	var asked struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	if json.Unmarshal(raw, &asked) != nil || asked.JSONRPC == "" || asked.Method == "" {
		return ""
	}
	return ": it is JSON-RPC " + strconv.Quote(asked.Method) + ", and this node serves HTTP+JSON (§11)"
}
