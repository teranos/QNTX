package ix

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pterm/pterm"
	"github.com/teranos/QNTX/pulse"
)

// ProgressEmitter extends pulse.ProgressEmitter with domain-specific methods
// for the attestation system. This allows ATS-specific handlers to use
// convenience methods like EmitAttestations while maintaining compatibility
// with the generic pulse infrastructure.
//
// Implementations include:
// - CLIEmitter: Pretty-printed terminal output using pterm
// - JSONEmitter: Structured JSON events for QNTX server consumption
type ProgressEmitter interface {
	pulse.ProgressEmitter

	// EmitAttestations announces batch generation of attestations.
	// This is a domain-specific convenience method that wraps EmitProgress
	// with attestation-specific semantics.
	EmitAttestations(count int, entities []AttestationEntity)
}

// JobBroadcaster is an optional interface that ProgressEmitter implementations
// can implement to support job tracking and broadcasting to UI clients.
// The WebSocketEmitter implements this interface to send job updates to connected clients.
type JobBroadcaster interface {
	// BroadcastJobUpdate sends a job update to all connected clients
	// The job parameter should be of type *async.Job but is interface{} to avoid import cycles
	BroadcastJobUpdate(job interface{})
}

// TaskTracker is an optional interface that ProgressEmitter implementations
// can implement to support fine-grained task tracking (e.g., individual items being processed).
// This enables UI visualizations like progress squares.
type TaskTracker interface {
	// AddTask registers a new task that will be tracked
	// taskID: unique identifier (e.g., item ID, entity ID)
	// taskName: display name (e.g., "Entity-A", "Item-123")
	AddTask(taskID string, taskName string)

	// UpdateTaskStatus updates a task's completion status
	// taskID: the task to update
	// completed: true if task finished successfully, false if failed
	// result: optional result summary (e.g., "Processed: 15 items")
	UpdateTaskStatus(taskID string, completed bool, result string)
}

// AttestationEntity represents a single attestation for batch emission
type AttestationEntity struct {
	Entity   string `json:"entity"`
	Relation string `json:"relation"`
	Value    string `json:"value"`
}

// ProgressEvent represents a structured JSON progress event
type ProgressEvent struct {
	Type      string                 `json:"type"`      // "stage", "attestations", "complete", "error", "info"
	Timestamp time.Time              `json:"timestamp"` // When this event occurred
	Data      map[string]interface{} `json:"data"`      // Event-specific data
}

// CLIEmitter outputs pretty-printed progress to terminal using pterm
type CLIEmitter struct {
	verbosity int
}

// NewCLIEmitter creates a CLI progress emitter for terminal output
func NewCLIEmitter(verbosity int) *CLIEmitter {
	return &CLIEmitter{verbosity: verbosity}
}

// EmitStage prints a stage announcement to terminal
func (e *CLIEmitter) EmitStage(stage string, message string) {
	pterm.Printf("🔄 %s: %s\n", pterm.LightCyan(stage), message)
}

// EmitProgress prints generic progress count (domain-agnostic from pulse.ProgressEmitter)
func (e *CLIEmitter) EmitProgress(count int, metadata map[string]interface{}) {
	// Check metadata for domain-specific type to customize output
	if itemType, ok := metadata["type"].(string); ok {
		pterm.Printf("✅ Processed %s %s\n", pterm.Green(fmt.Sprintf("%d", count)), itemType)
	} else {
		pterm.Printf("✅ Processed %s items\n", pterm.Green(fmt.Sprintf("%d", count)))
	}
}

// EmitAttestations prints attestation batch count (ATS domain-specific convenience method)
func (e *CLIEmitter) EmitAttestations(count int, entities []AttestationEntity) {
	pterm.Printf("✅ Generated %s attestations\n", pterm.Green(fmt.Sprintf("%d", count)))
}

// EmitComplete prints completion summary
func (e *CLIEmitter) EmitComplete(summary map[string]interface{}) {
	pterm.Success.Println("Processing complete!")
	if e.verbosity >= 1 {
		for key, value := range summary {
			pterm.Printf("  %s: %v\n", key, value)
		}
	}
}

// EmitError prints an error
func (e *CLIEmitter) EmitError(stage string, err error) {
	pterm.Error.Printf("Error in %s: %v\n", stage, err)
}

// EmitInfo prints informational message
func (e *CLIEmitter) EmitInfo(message string) {
	if e.verbosity >= 1 {
		pterm.Info.Println(message)
	}
}

// BroadcastLLMStream prints streaming LLM output to terminal
// This implements the same interface as WebSocketEmitter, allowing terminal commands
// to show live token-by-token LLM generation just like the UI
func (e *CLIEmitter) BroadcastLLMStream(jobID, taskID, content string, done bool, err error, model, stage string) {
	if err != nil {
		pterm.Error.Printf("Streaming error: %v\n", err)
		return
	}

	if content != "" {
		// Print content without newline so tokens append on same line
		fmt.Print(pterm.LightCyan(content))
	}

	if done {
		// Print newline after stream completes
		fmt.Println()
	}
}

// JSONEmitter outputs structured JSON events to stdout for QNTX server consumption
type JSONEmitter struct {
	encoder *json.Encoder
}

// NewJSONEmitter creates a JSON progress emitter for structured output
func NewJSONEmitter() *JSONEmitter {
	return &JSONEmitter{
		encoder: json.NewEncoder(os.Stdout),
	}
}

// EmitStage emits a stage event as JSON
func (e *JSONEmitter) EmitStage(stage string, message string) {
	event := ProgressEvent{
		Type:      "stage",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"stage":   stage,
			"message": message,
		},
	}
	e.encoder.Encode(event)
}

// EmitProgress emits a generic progress event as JSON (domain-agnostic from pulse.ProgressEmitter)
func (e *JSONEmitter) EmitProgress(count int, metadata map[string]interface{}) {
	data := map[string]interface{}{
		"count": count,
	}
	// Merge metadata into data
	for k, v := range metadata {
		data[k] = v
	}
	event := ProgressEvent{
		Type:      "progress",
		Timestamp: time.Now(),
		Data:      data,
	}
	e.encoder.Encode(event)
}

// EmitAttestations emits an attestations batch event as JSON (ATS domain-specific)
func (e *JSONEmitter) EmitAttestations(count int, entities []AttestationEntity) {
	event := ProgressEvent{
		Type:      "attestations",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"count":    count,
			"entities": entities,
		},
	}
	e.encoder.Encode(event)
}

// EmitComplete emits a completion event as JSON
func (e *JSONEmitter) EmitComplete(summary map[string]interface{}) {
	event := ProgressEvent{
		Type:      "complete",
		Timestamp: time.Now(),
		Data:      summary,
	}
	e.encoder.Encode(event)
}

// EmitError emits an error event as JSON
func (e *JSONEmitter) EmitError(stage string, err error) {
	event := ProgressEvent{
		Type:      "error",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"stage": stage,
			"error": err.Error(),
		},
	}
	e.encoder.Encode(event)
}

// EmitInfo emits an info event as JSON
func (e *JSONEmitter) EmitInfo(message string) {
	event := ProgressEvent{
		Type:      "info",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message": message,
		},
	}
	e.encoder.Encode(event)
}
