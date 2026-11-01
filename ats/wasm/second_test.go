package wasm

import (
	"encoding/json"
	"strings"
	"testing"
)

// A subsystem compiled in brings a wasm module of its own and instantiates it
// with the same memory protocol the embedded one speaks. Here the embedded
// bytes stand in for a subsystem's: a second engine, apart from the singleton.
func TestASecondEngineSpeaksTheSameProtocol(t *testing.T) {
	second, err := NewEngine(wasmBytes, "second")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	first, err := GetEngine()
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}
	if second == first {
		t.Fatal("NewEngine handed back the singleton")
	}
	raw, err := second.Call("parse_ax_query", "ALICE is author")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var parsed struct {
		Subjects []string `json:"subjects"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, raw)
	}
	if len(parsed.Subjects) != 1 || parsed.Subjects[0] != "ALICE" {
		t.Fatalf("subjects %v", parsed.Subjects)
	}
}

// Bytes that are no module are refused with the name the caller gave them.
func TestBytesThatAreNoModuleAreRefusedByName(t *testing.T) {
	_, err := NewEngine([]byte("not wasm"), "quotes")
	if err == nil || !strings.Contains(err.Error(), "quotes") {
		t.Fatalf("NewEngine over nothing: %v", err)
	}
}
