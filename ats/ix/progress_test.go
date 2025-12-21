package ix

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// TestCLIEmitter_EmitStage verifies CLIEmitter doesn't panic on stage emission
func TestCLIEmitter_EmitStage(t *testing.T) {
	emitter := NewCLIEmitter(2)

	// Should not panic
	emitter.EmitStage("test_stage", "test message")
}

// TestCLIEmitter_EmitAttestations verifies attestation emission works
func TestCLIEmitter_EmitAttestations(t *testing.T) {
	emitter := NewCLIEmitter(2)

	entities := []AttestationEntity{
		{Entity: "entity:123", Relation: "has_attribute", Value: "property_a"},
	}

	// Should not panic
	emitter.EmitAttestations(1, entities)
}

// TestCLIEmitter_EmitComplete verifies completion emission
func TestCLIEmitter_EmitComplete(t *testing.T) {
	emitter := NewCLIEmitter(2)

	summary := map[string]interface{}{
		"entities_processed": 10,
		"matched":            3,
	}

	// Should not panic
	emitter.EmitComplete(summary)
}

// TestCLIEmitter_EmitError verifies error emission
func TestCLIEmitter_EmitError(t *testing.T) {
	emitter := NewCLIEmitter(2)

	// Should not panic
	emitter.EmitError("scoring", errors.New("test error"))
}

// TestCLIEmitter_VerbosityFiltering verifies info is filtered by verbosity
func TestCLIEmitter_VerbosityFiltering(t *testing.T) {
	// Verbosity 0 - info should be filtered
	emitter0 := NewCLIEmitter(0)
	emitter0.EmitInfo("should not show")

	// Verbosity 1 - info should show
	emitter1 := NewCLIEmitter(1)
	emitter1.EmitInfo("should show")

	// Just verify no panics - visual output not tested
}

// TestJSONEmitter_EventStructure verifies JSON structure is correct
func TestJSONEmitter_EventStructure(t *testing.T) {
	var buf bytes.Buffer
	emitter := &JSONEmitter{encoder: json.NewEncoder(&buf)}

	// Emit stage event
	emitter.EmitStage("extraction", "Extracting data")

	// Parse JSON
	var event ProgressEvent
	if err := json.NewDecoder(&buf).Decode(&event); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	// Verify structure
	if event.Type != "stage" {
		t.Errorf("Expected type 'stage', got '%s'", event.Type)
	}

	if event.Data["stage"] != "extraction" {
		t.Errorf("Expected stage 'extraction', got '%v'", event.Data["stage"])
	}

	if event.Data["message"] != "Extracting data" {
		t.Errorf("Expected message 'Extracting data', got '%v'", event.Data["message"])
	}

	if event.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

// TestJSONEmitter_AttestationsEvent verifies attestations JSON structure
func TestJSONEmitter_AttestationsEvent(t *testing.T) {
	var buf bytes.Buffer
	emitter := &JSONEmitter{encoder: json.NewEncoder(&buf)}

	entities := []AttestationEntity{
		{Entity: "entity:123", Relation: "has_attribute", Value: "property_a"},
	}

	emitter.EmitAttestations(1, entities)

	var event ProgressEvent
	if err := json.NewDecoder(&buf).Decode(&event); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if event.Type != "attestations" {
		t.Errorf("Expected type 'attestations', got '%s'", event.Type)
	}

	count, ok := event.Data["count"].(float64) // JSON numbers decode as float64
	if !ok || int(count) != 1 {
		t.Errorf("Expected count 1, got %v", event.Data["count"])
	}
}

// TestJSONEmitter_ErrorEvent verifies error JSON structure
func TestJSONEmitter_ErrorEvent(t *testing.T) {
	var buf bytes.Buffer
	emitter := &JSONEmitter{encoder: json.NewEncoder(&buf)}

	emitter.EmitError("scoring", errors.New("test error"))

	var event ProgressEvent
	if err := json.NewDecoder(&buf).Decode(&event); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if event.Type != "error" {
		t.Errorf("Expected type 'error', got '%s'", event.Type)
	}

	if event.Data["stage"] != "scoring" {
		t.Errorf("Expected stage 'scoring', got '%v'", event.Data["stage"])
	}

	if event.Data["error"] != "test error" {
		t.Errorf("Expected error 'test error', got '%v'", event.Data["error"])
	}
}

// TestJSONEmitter_CompleteEvent verifies completion JSON structure
func TestJSONEmitter_CompleteEvent(t *testing.T) {
	var buf bytes.Buffer
	emitter := &JSONEmitter{encoder: json.NewEncoder(&buf)}

	summary := map[string]interface{}{
		"source_id":          "source:123",
		"entities_processed": 10,
		"matched":            3,
	}

	emitter.EmitComplete(summary)

	var event ProgressEvent
	if err := json.NewDecoder(&buf).Decode(&event); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if event.Type != "complete" {
		t.Errorf("Expected type 'complete', got '%s'", event.Type)
	}

	if event.Data["source_id"] != "source:123" {
		t.Errorf("Expected source_id 'source:123', got '%v'", event.Data["source_id"])
	}
}
