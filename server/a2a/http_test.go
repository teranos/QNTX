package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// sent is a SendMessageRequest that sets what the spec requires.
const sent = `{"message": {"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": "hi"}]}}`

// One request per operation in scope, on the route §5.3 gives it, with what
// the spec requires of it.
var asked = map[string]struct{ method, path, body string }{
	"SendMessage":          {"POST", "/message:send", sent},
	"SendStreamingMessage": {"POST", "/message:stream", sent},
	"GetTask":              {"GET", "/tasks/abc", ""},
	"ListTasks":            {"GET", "/tasks", ""},
	"CancelTask":           {"POST", "/tasks/abc:cancel", ""},
	"GetExtendedAgentCard": {"GET", "/extendedAgentCard", ""},
}

// status is a google.rpc.Status as §11.6 writes it.
type status struct {
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type            string `json:"@type"`
			Reason          string `json:"reason"`
			Domain          string `json:"domain"`
			FieldViolations []struct {
				Field string `json:"field"`
			} `json:"fieldViolations"`
		} `json:"details"`
	} `json:"error"`
}

func serve(t *testing.T, answer Answer, method, path, body string, header map[string]string) (*httptest.ResponseRecorder, status) {
	t.Helper()
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	HTTP(operations, answer, func(what string, err error) { t.Errorf("%s was not delivered: %v", what, err) }).ServeHTTP(w, r)
	var said status
	_ = json.Unmarshal(w.Body.Bytes(), &said)
	return w, said
}

func unsupported(_ context.Context, op Operation, _ proto.Message) (proto.Message, *Error) {
	return nil, Unsupported(op)
}

var v1 = map[string]string{"A2A-Version": "1.0"}

// §3.6.2: an empty A2A-Version is 0.3, which this node does not speak, and the
// refusal is VersionNotSupportedError as §11.6 writes an A2A error.
func TestNoVersionIsZeroThreeAndRefused(t *testing.T) {
	w, said := serve(t, unsupported, "GET", "/tasks/abc", "", nil)
	if w.Code != http.StatusBadRequest || said.Error.Status != "FAILED_PRECONDITION" ||
		len(said.Error.Details) != 1 || said.Error.Details[0].Reason != "VERSION_NOT_SUPPORTED" ||
		said.Error.Details[0].Domain != "a2a-protocol.org" || said.Error.Details[0].Type != "type.googleapis.com/google.rpc.ErrorInfo" {
		t.Errorf("no version answered %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(said.Error.Message, "0.3") {
		t.Errorf("the refusal does not say 0.3 was read: %q", said.Error.Message)
	}
	if got := w.Header().Get("Content-Type"); got != contentType {
		t.Errorf("content type is %q", got)
	}
}

// §3.6: Major.Minor alone, from the header or the request parameter.
func TestVersionIsMajorMinor(t *testing.T) {
	for _, asked := range []struct{ path, header string }{
		{"/tasks/abc", "1.0"},
		{"/tasks/abc", "1.0.1"},
		{"/tasks/abc?A2A-Version=1.0", ""},
	} {
		header := map[string]string{}
		if asked.header != "" {
			header["A2A-Version"] = asked.header
		}
		_, said := serve(t, unsupported, "GET", asked.path, "", header)
		if len(said.Error.Details) != 1 || said.Error.Details[0].Reason != "UNSUPPORTED_OPERATION" {
			t.Errorf("%s with %q was refused by version: %+v", asked.path, asked.header, said)
		}
	}
}

// A body that is not the request message is a validation error (§3.3.2), not
// one of A2A's own, so it carries no ErrorInfo.
func TestABodyThatIsNotTheRequestIsInvalid(t *testing.T) {
	w, said := serve(t, unsupported, "POST", "/message:send", `{"message": 7}`, v1)
	if w.Code != http.StatusBadRequest || said.Error.Status != "INVALID_ARGUMENT" || len(said.Error.Details) != 0 {
		t.Errorf("a bad body answered %d %s", w.Code, w.Body.String())
	}
}

// §3.3.2 and §5.7: a request that does not set what the spec requires is a
// validation error naming each field, and no operation is asked.
func TestARequestLackingWhatTheSpecRequiresIsInvalid(t *testing.T) {
	for body, want := range map[string][]string{
		`{}`:             {"message"},
		`{"message":{}}`: {"message.message_id", "message.role", "message.parts"},
	} {
		reached := false
		answer := func(_ context.Context, op Operation, _ proto.Message) (proto.Message, *Error) {
			reached = true
			return nil, Unsupported(op)
		}
		w, said := serve(t, answer, "POST", "/message:send", body, v1)
		if reached || w.Code != http.StatusBadRequest || said.Error.Status != "INVALID_ARGUMENT" || len(said.Error.Details) != 1 {
			t.Errorf("%s answered %d %s", body, w.Code, w.Body.String())
			continue
		}
		var named []string
		for _, v := range said.Error.Details[0].FieldViolations {
			named = append(named, v.Field)
		}
		if strings.Join(named, " ") != strings.Join(want, " ") {
			t.Errorf("%s named %v, not %v", body, named, want)
		}
	}
	w, said := serve(t, unsupported, "POST", "/message:send", sent, v1)
	if w.Code != http.StatusBadRequest || len(said.Error.Details) != 1 || said.Error.Details[0].Reason != "UNSUPPORTED_OPERATION" {
		t.Errorf("a request with what the spec requires answered %d %s", w.Code, w.Body.String())
	}
}

// Every operation in scope, with a tenant and without, reaches what answers,
// with its path parameters on its request, and is UnsupportedOperationError
// until the node does it.
func TestEveryOperationReachesTheAnswerAndIsUnsupported(t *testing.T) {
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		for _, tenant := range []string{"", "/garden"} {
			var reached string
			var request proto.Message
			answer := func(_ context.Context, op Operation, r proto.Message) (proto.Message, *Error) {
				reached, request = op.Name, r
				return nil, Unsupported(op)
			}
			route := asked[op.Name]
			w, said := serve(t, answer, route.method, tenant+route.path, route.body, v1)
			if reached != op.Name {
				t.Errorf("%s %s reached %q", route.method, tenant+route.path, reached)
				continue
			}
			if w.Code != http.StatusBadRequest || len(said.Error.Details) != 1 || said.Error.Details[0].Reason != "UNSUPPORTED_OPERATION" {
				t.Errorf("%s answered %d %s", op.Name, w.Code, w.Body.String())
			}
			fields := request.ProtoReflect().Descriptor().Fields()
			if tenant != "" && request.ProtoReflect().Get(fields.ByName("tenant")).String() != "garden" {
				t.Errorf("%s did not take its tenant from the path", op.Name)
			}
			if id := fields.ByName(protoreflect.Name("id")); id != nil && strings.Contains(route.path, "abc") &&
				request.ProtoReflect().Get(id).String() != "abc" {
				t.Errorf("%s did not take its id from the path", op.Name)
			}
		}
	}
}

// §11.5: a GET carries its request in camelCase query parameters.
func TestAGetReadsItsQuery(t *testing.T) {
	var request proto.Message
	answer := func(_ context.Context, op Operation, r proto.Message) (proto.Message, *Error) {
		request = r
		return nil, Unsupported(op)
	}
	serve(t, answer, "GET", "/tasks?contextId=c1&pageSize=5&status=TASK_STATE_WORKING&includeArtifacts=true", "", v1)
	if request == nil {
		t.Fatal("ListTasks was not reached")
	}
	m := request.ProtoReflect()
	fields := m.Descriptor().Fields()
	if m.Get(fields.ByName("context_id")).String() != "c1" || m.Get(fields.ByName("page_size")).Int() != 5 ||
		string(fields.ByName("status").Enum().Values().ByNumber(m.Get(fields.ByName("status")).Enum()).Name()) != "TASK_STATE_WORKING" ||
		!m.Get(fields.ByName("include_artifacts")).Bool() {
		t.Errorf("ListTasks read %v", m)
	}
}

// A path no operation in scope answers is not found, and that includes the
// operations named out of scope.
func TestWhatIsNoOperationIsNotFound(t *testing.T) {
	for _, r := range []struct{ method, path string }{
		{"GET", "/nothing"},
		{"DELETE", "/tasks/abc"},
		{"GET", "/tasks/abc:subscribe"},
		{"GET", "/tasks/abc/pushNotificationConfigs"},
	} {
		w, said := serve(t, unsupported, r.method, r.path, "", v1)
		if w.Code != http.StatusNotFound || said.Error.Status != "NOT_FOUND" {
			t.Errorf("%s %s answered %d %s", r.method, r.path, w.Code, w.Body.String())
		}
	}
}

func TestMatchPath(t *testing.T) {
	for _, c := range []struct {
		template, path string
		ok             bool
		params         map[string]string
	}{
		{"/tasks/{id}:cancel", "/tasks/abc:cancel", true, map[string]string{"id": "abc"}},
		{"/tasks/{id}:cancel", "/tasks/:cancel", false, nil},
		{"/tasks/{id}:cancel", "/tasks/abc", false, nil},
		{"/tasks/{id}", "/tasks/abc:subscribe", false, nil},
		{"/tasks/{id}", "/tasks/abc", true, map[string]string{"id": "abc"}},
		{"/{tenant}/tasks", "/garden/tasks", true, map[string]string{"tenant": "garden"}},
		{"/message:send", "/message:send", true, map[string]string{}},
		{"/message:send", "/message:stream", false, nil},
	} {
		params, ok := matchPath(c.template, c.path)
		if ok != c.ok || (ok && len(params) != len(c.params)) {
			t.Errorf("%s on %s: %v %v", c.template, c.path, ok, params)
			continue
		}
		for k, v := range c.params {
			if params[k] != v {
				t.Errorf("%s on %s: %s is %q", c.template, c.path, k, params[k])
			}
		}
	}
}
