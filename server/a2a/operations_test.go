package a2a

import (
	"slices"
	"testing"
)

// §5.3 Method Mapping Reference of specification.md at v1.0.1, for the
// operations in scope: the JSON-RPC method and the REST endpoint as the
// document writes them (specification.md:1164-1168, 1174).
var methodMapping = []struct {
	name    string
	jsonrpc string
	http    Route
}{
	{"SendMessage", "SendMessage", Route{"POST", "/message:send"}},
	{"SendStreamingMessage", "SendStreamingMessage", Route{"POST", "/message:stream"}},
	{"GetTask", "GetTask", Route{"GET", "/tasks/{id}"}},
	{"ListTasks", "ListTasks", Route{"GET", "/tasks"}},
	{"CancelTask", "CancelTask", Route{"POST", "/tasks/{id}:cancel"}},
	{"GetExtendedAgentCard", "GetExtendedAgentCard", Route{"GET", "/extendedAgentCard"}},
}

// The table read from a2a.proto is what §5.3 says, operation by operation, and
// under /{tenant} the same route.
func TestOperationsAreTheMethodMapping(t *testing.T) {
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != len(methodMapping) {
		t.Fatalf("%d operations are in scope, and §5.3 maps %d of them", len(operations), len(methodMapping))
	}
	for i, want := range methodMapping {
		got := operations[i]
		if got.Name != want.name || got.JSONRPC != want.jsonrpc || got.HTTP != want.http {
			t.Errorf("%s reads %s %+v, and §5.3 says %s %s %+v", got.Name, got.JSONRPC, got.HTTP, want.name, want.jsonrpc, want.http)
		}
		tenant := Route{Method: want.http.Method, Path: "/{tenant}" + want.http.Path}
		if got.Tenant != tenant {
			t.Errorf("%s under a tenant is %+v, not %+v", got.Name, got.Tenant, tenant)
		}
	}
}

// What each operation takes and gives, and which stream (§3.1).
func TestOperationsCarryTheirMessages(t *testing.T) {
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{
		"SendMessage":          {"lf.a2a.v1.SendMessageRequest", "lf.a2a.v1.SendMessageResponse", ""},
		"SendStreamingMessage": {"lf.a2a.v1.SendMessageRequest", "lf.a2a.v1.StreamResponse", "streams"},
		"GetTask":              {"lf.a2a.v1.GetTaskRequest", "lf.a2a.v1.Task", ""},
		"ListTasks":            {"lf.a2a.v1.ListTasksRequest", "lf.a2a.v1.ListTasksResponse", ""},
		"CancelTask":           {"lf.a2a.v1.CancelTaskRequest", "lf.a2a.v1.Task", ""},
		"GetExtendedAgentCard": {"lf.a2a.v1.GetExtendedAgentCardRequest", "lf.a2a.v1.AgentCard", ""},
	}
	for _, op := range operations {
		w := want[op.Name]
		if string(op.Request) != w[0] || string(op.Response) != w[1] || op.Streams != (w[2] == "streams") {
			t.Errorf("%s takes %s, gives %s, streams %v", op.Name, op.Request, op.Response, op.Streams)
		}
	}
}

// Everything the service declares is either in scope or named out of it, so an
// rpc a future pin adds is not left out unseen.
func TestEveryRPCIsInScopeOrNamedOut(t *testing.T) {
	declared, err := Declared()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		inScope := slices.ContainsFunc(methodMapping, func(m struct {
			name    string
			jsonrpc string
			http    Route
		}) bool {
			return m.name == name
		})
		if !inScope && !slices.Contains(OutOfScope, name) {
			t.Errorf("%s is declared and neither in scope nor out of it", name)
		}
	}
	if len(declared) != len(methodMapping)+len(OutOfScope) {
		t.Errorf("A2AService declares %d rpcs: %v", len(declared), declared)
	}
}

func TestPlain(t *testing.T) {
	for in, want := range map[string]string{
		"/tasks/{id=*}":                                     "/tasks/{id}",
		"/{tenant}/tasks/{id=*}:cancel":                     "/{tenant}/tasks/{id}:cancel",
		"/tasks/{task_id=*}/pushNotificationConfigs/{id=*}": "/tasks/{task_id}/pushNotificationConfigs/{id}",
		"/v1/{name=projects/*}":                             "/v1/{name=projects/*}",
		"/message:send":                                     "/message:send",
	} {
		if got := plain(in); got != want {
			t.Errorf("plain(%q) is %q, not %q", in, got, want)
		}
	}
}
