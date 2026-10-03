package a2a

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

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

// HTTP serves the operations on their routes, relative to wherever it is
// mounted. Every request is read the same way before any operation answers.
func HTTP(operations []Operation, answer Answer, undelivered Undelivered) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, params, found := match(operations, r.Method, r.URL.Path)
		if !found {
			notFound(r.Method+" "+r.URL.Path+" is no A2A operation").write(w, undelivered)
			return
		}
		if refused := version(r); refused != nil {
			refused.write(w, undelivered)
			return
		}
		request, refused := read(op, r, params)
		if refused != nil {
			refused.write(w, undelivered)
			return
		}
		if refused := lacking(op, request); refused != nil {
			refused.write(w, undelivered)
			return
		}
		response, refused := answer(r.Context(), op, request)
		if refused != nil {
			refused.write(w, undelivered)
			return
		}
		if op.Streams {
			// A stream is Server-Sent Events (§11.7), and no operation here
			// streams yet.
			(&Error{Name: "InvalidAgentResponseError", Message: op.Name + " answered without a stream"}).write(w, undelivered)
			return
		}
		body, err := protojson.Marshal(response)
		if err != nil {
			(&Error{Name: "InvalidAgentResponseError", Message: err.Error()}).write(w, undelivered)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if _, err := w.Write(body); err != nil {
			undelivered(op.Name, err)
		}
	})
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
		body, err := json.Marshal(queried(op.request, r))
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
// the string as written otherwise (§11.5).
func queried(message protoreflect.MessageDescriptor, r *http.Request) map[string]any {
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
	return values
}
