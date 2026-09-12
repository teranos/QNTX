package server

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// Proto is the single source of truth for a type the browser and the node both
// speak (ADR-006). Go keeps its own struct for the json tags rather than using
// the generated one, which leaves two declarations of one shape — and two
// declarations drift without anybody being told.
//
// So the tags are held against the proto here. A field added to one and not the
// other fails, which is the whole of what the mirror costs.

const watcherProto = "../plugin/grpc/protocol/server.proto"

// fieldsOf is the field names one proto message declares, in order.
func fieldsOf(t *testing.T, path, message string) []string {
	t.Helper()

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the proto could not be read: %v", err)
	}

	var named []string
	inside := false
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "message "+message+" {":
			inside = true
			continue
		case inside && trimmed == "}":
			return named
		case !inside || trimmed == "" || strings.HasPrefix(trimmed, "//"):
			continue
		}

		// `optional string last_error = 20;  // comment` — the name is the
		// word before the `=`.
		declaration, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		words := strings.Fields(declaration)
		if len(words) < 2 {
			continue
		}
		named = append(named, words[len(words)-1])
	}

	t.Fatalf("%s declares no message %s", path, message)
	return nil
}

// jsonNamesOf is what a struct calls its fields on the wire, in order.
func jsonNamesOf(t *testing.T, of any) []string {
	t.Helper()

	shape := reflect.TypeOf(of)
	named := make([]string, 0, shape.NumField())
	for i := range shape.NumField() {
		tag := shape.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		named = append(named, name)
	}
	return named
}

func TestWatcherResponseIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, watcherProto, "WatcherResponse")
	tagged := jsonNamesOf(t, WatcherResponse{})

	if len(declared) != len(tagged) {
		t.Fatalf("proto declares %d fields and the struct tags %d:\n  proto:  %v\n  struct: %v",
			len(declared), len(tagged), declared, tagged)
	}
	for i := range declared {
		if declared[i] != tagged[i] {
			t.Errorf("field %d: proto says %q, the struct says %q", i+1, declared[i], tagged[i])
		}
	}
}

func TestWatcherFireIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, watcherProto, "WatcherFire")
	tagged := jsonNamesOf(t, WatcherFire{})

	if len(declared) != len(tagged) {
		t.Fatalf("proto declares %d fields and the struct tags %d:\n  proto:  %v\n  struct: %v",
			len(declared), len(tagged), declared, tagged)
	}
	for i := range declared {
		if declared[i] != tagged[i] {
			t.Errorf("field %d: proto says %q, the struct says %q", i+1, declared[i], tagged[i])
		}
	}
}
