package a2a

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/parity"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// specification is A2A's document as pinned beside a2a.proto: make says
// fetches it by its hash (nix/references/a2a.nix).
const specification = "../parity/a2a_v1.0.1_3303592/specification.md"

// mappedRow is one row of §5.3 Method Mapping Reference: the JSON-RPC method
// and the REST endpoint the document gives an rpc, by its gRPC method.
type mappedRow struct {
	jsonrpc string
	http    Route
}

// methodMapping is §5.3 as the document writes it, read from the document.
func methodMapping(t *testing.T) map[string]mappedRow {
	t.Helper()
	raw, err := os.ReadFile(specification)
	if err != nil {
		t.Fatalf("the pinned specification did not read: %v", err)
	}
	_, section, found := strings.Cut(string(raw), "### 5.3. Method Mapping Reference\n")
	if !found {
		t.Fatalf("%s has no §5.3 Method Mapping Reference", specification)
	}
	section, _, _ = strings.Cut(section, "\n### ")
	rows := map[string]mappedRow{}
	var header []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		var cells []string
		for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.Trim(strings.TrimSpace(cell), "`"))
		}
		switch {
		case header == nil:
			header = cells
			continue
		case strings.HasPrefix(cells[0], ":"):
			continue
		}
		row := map[string]string{}
		for i, cell := range cells {
			row[header[i]] = cell
		}
		method, path, ok := strings.Cut(row["REST Endpoint"], " ")
		if !ok {
			t.Fatalf("§5.3 writes %s's REST endpoint as %q", row["gRPC Method"], row["REST Endpoint"])
		}
		rows[row["gRPC Method"]] = mappedRow{jsonrpc: row["JSON-RPC Method"], http: Route{method, path}}
	}
	if len(rows) == 0 {
		t.Fatalf("§5.3 of %s maps nothing", specification)
	}
	return rows
}

// The table read from a2a.proto is what §5.3 says, operation by operation, and
// under /{tenant} the same route.
func TestOperationsAreTheMethodMapping(t *testing.T) {
	mapping := methodMapping(t)
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != len(mapping)-len(OutOfScope) {
		t.Fatalf("%d operations are in scope, and §5.3 maps %d with %d out of scope", len(operations), len(mapping), len(OutOfScope))
	}
	for _, got := range operations {
		want, ok := mapping[got.Name]
		if !ok {
			t.Errorf("§5.3 does not map %s", got.Name)
			continue
		}
		if got.JSONRPC != want.jsonrpc || got.HTTP.Method != want.http.Method || unnamed(got.HTTP.Path) != unnamed(want.http.Path) {
			t.Errorf("%s reads %s %+v, and §5.3 says %s %+v", got.Name, got.JSONRPC, got.HTTP, want.jsonrpc, want.http)
		}
		tenant := Route{Method: want.http.Method, Path: "/{tenant}" + want.http.Path}
		if got.Tenant.Method != tenant.Method || unnamed(got.Tenant.Path) != unnamed(tenant.Path) {
			t.Errorf("%s under a tenant is %+v, not %+v", got.Name, got.Tenant, tenant)
		}
	}
}

// unnamed is a path with each parameter's name left out: {id} and {task_id}
// are one segment.
func unnamed(path string) string {
	var b strings.Builder
	for {
		open := strings.Index(path, "{")
		closing := strings.Index(path, "}")
		if open < 0 || closing < open {
			b.WriteString(path)
			return b.String()
		}
		b.WriteString(path[:open] + "{}")
		path = path[closing+1:]
	}
}

// §5.3 names a push notification config's segments {id} and {configId}, and
// a2a.proto {task_id} and {id}: the same task, then the same config. "spec/a2a.proto
// is the single authoritative normative definition of all protocol data objects
// and request/response messages" (specification.md:107), so a segment fills the
// field a2a.proto names.
func TestAPushConfigSegmentIsNamedAsA2AProtoNamesIt(t *testing.T) {
	mapping := methodMapping(t)
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		if !strings.Contains(op.HTTP.Path, "pushNotificationConfigs") {
			continue
		}
		if !strings.HasPrefix(op.HTTP.Path, "/tasks/{task_id}/pushNotificationConfigs") {
			t.Errorf("%s is routed %s, not under its task_id", op.Name, op.HTTP.Path)
		}
		if !strings.HasPrefix(mapping[op.Name].http.Path, "/tasks/{id}/pushNotificationConfigs") {
			t.Errorf("§5.3 routes %s %s: the names no longer differ", op.Name, mapping[op.Name].http.Path)
		}
	}
}

// "The parts where you think that the spec disagrees with itself, those
// specific parts will not be in scope": SubscribeToTask is one, a2a.proto
// routing it as GET and §5.3 as POST.
func TestSubscribeToTaskIsWhereTheSpecDisagreesWithItself(t *testing.T) {
	want := methodMapping(t)["SubscribeToTask"]
	files, refused := parity.Descriptors("a2a")
	if refused != nil {
		t.Fatal(refused.GetSays())
	}
	var got Route
	for _, file := range files {
		if service := file.Services().ByName(protoreflect.FullName(Service).Name()); service != nil {
			rule, err := httpRule(service.Methods().ByName("SubscribeToTask"))
			if err != nil {
				t.Fatal(err)
			}
			got = routeOf(rule)
		}
	}
	if got.Path != want.http.Path || got.Method == want.http.Method {
		t.Errorf("a2a.proto routes SubscribeToTask %+v and §5.3 %+v: they no longer disagree as OutOfScope says", got, want.http)
	}
}

// What each operation takes and gives, and which stream (§3.1).
func TestOperationsCarryTheirMessages(t *testing.T) {
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{
		"SendMessage":                      {"lf.a2a.v1.SendMessageRequest", "lf.a2a.v1.SendMessageResponse", ""},
		"SendStreamingMessage":             {"lf.a2a.v1.SendMessageRequest", "lf.a2a.v1.StreamResponse", "streams"},
		"GetTask":                          {"lf.a2a.v1.GetTaskRequest", "lf.a2a.v1.Task", ""},
		"ListTasks":                        {"lf.a2a.v1.ListTasksRequest", "lf.a2a.v1.ListTasksResponse", ""},
		"CancelTask":                       {"lf.a2a.v1.CancelTaskRequest", "lf.a2a.v1.Task", ""},
		"GetExtendedAgentCard":             {"lf.a2a.v1.GetExtendedAgentCardRequest", "lf.a2a.v1.AgentCard", ""},
		"CreateTaskPushNotificationConfig": {"lf.a2a.v1.TaskPushNotificationConfig", "lf.a2a.v1.TaskPushNotificationConfig", ""},
		"GetTaskPushNotificationConfig":    {"lf.a2a.v1.GetTaskPushNotificationConfigRequest", "lf.a2a.v1.TaskPushNotificationConfig", ""},
		"ListTaskPushNotificationConfigs":  {"lf.a2a.v1.ListTaskPushNotificationConfigsRequest", "lf.a2a.v1.ListTaskPushNotificationConfigsResponse", ""},
		"DeleteTaskPushNotificationConfig": {"lf.a2a.v1.DeleteTaskPushNotificationConfigRequest", "google.protobuf.Empty", ""},
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
	mapping := methodMapping(t)
	operations, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if _, ok := mapping[name]; !ok {
			t.Errorf("%s is declared and §5.3 does not map it", name)
		}
		inScope := slices.ContainsFunc(operations, func(op Operation) bool { return op.Name == name })
		if inScope == slices.Contains(OutOfScope, name) {
			t.Errorf("%s is declared and in scope %v, out of scope %v", name, inScope, !inScope)
		}
	}
	if len(declared) != len(mapping) {
		t.Errorf("A2AService declares %d rpcs, and §5.3 maps %d: %v", len(declared), len(mapping), declared)
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
