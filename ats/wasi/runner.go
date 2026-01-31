package wasi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WASIRunner executes QNTX WASI modules for attestation verification
type WASIRunner struct {
	runtime wazero.Runtime
	module  wazero.CompiledModule
}

// NewWASIRunner creates a new WASI runner with the QNTX module
func NewWASIRunner(wasmBytes []byte) (*WASIRunner, error) {
	// Create a new WebAssembly runtime
	r := wazero.NewRuntime(context.Background())

	// Instantiate WASI functions
	if _, err := wasi_snapshot_preview1.Instantiate(context.Background(), r); err != nil {
		return nil, errors.Wrap(err, "failed to instantiate WASI")
	}

	// Compile the WASM module
	compiled, err := r.CompileModule(context.Background(), wasmBytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to compile WASM module")
	}

	return &WASIRunner{
		runtime: r,
		module:  compiled,
	}, nil
}

// VerifyCommand represents the WASI verify command structure
type VerifyCommand struct {
	Cmd         string          `json:"cmd"`
	Attestation WASIAttestation `json:"attestation"`
	Subject     *string         `json:"subject,omitempty"`
	Predicate   *string         `json:"predicate,omitempty"`
	Context     *string         `json:"context,omitempty"`
	Actor       *string         `json:"actor,omitempty"`
}

// WASIAttestation matches the WASI module's attestation format
type WASIAttestation struct {
	ID         string                 `json:"id"`
	Subjects   []string               `json:"subjects"`
	Predicates []string               `json:"predicates"`
	Contexts   []string               `json:"contexts"`
	Actors     []string               `json:"actors"`
	Timestamp  int64                  `json:"timestamp"`
	Source     string                 `json:"source"`
	Attributes map[string]interface{} `json:"attributes"`
}

// WASIResponse represents the response from WASI execution
type WASIResponse struct {
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result,omitempty"`
	Message string          `json:"message,omitempty"`
}

// ConvertToWASI converts a Go attestation to WASI format
func ConvertToWASI(as *types.As) WASIAttestation {
	return WASIAttestation{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.Unix(),
		Source:     as.Source,
		Attributes: as.Attributes,
	}
}

// Verify runs the WASI verification against an attestation
func (w *WASIRunner) Verify(ctx context.Context, as *types.As, filter *types.AxFilter) (bool, error) {
	// Convert attestation to WASI format
	wasiAtt := ConvertToWASI(as)

	// Build the verify command
	cmd := VerifyCommand{
		Cmd:         "Verify",
		Attestation: wasiAtt,
	}

	// Apply filters if provided
	if filter != nil {
		if len(filter.Subjects) > 0 {
			subject := filter.Subjects[0] // Simplified - could enhance
			cmd.Subject = &subject
		}
		if len(filter.Predicates) > 0 {
			predicate := filter.Predicates[0]
			cmd.Predicate = &predicate
		}
		if len(filter.Contexts) > 0 {
			context := filter.Contexts[0]
			cmd.Context = &context
		}
		if len(filter.Actors) > 0 {
			actor := filter.Actors[0]
			cmd.Actor = &actor
		}
	}

	// Marshal command to JSON
	input, err := json.Marshal(cmd)
	if err != nil {
		return false, errors.Wrap(err, "failed to marshal command")
	}

	// Execute WASI module with input
	output, err := w.executeWASI(ctx, input)
	if err != nil {
		return false, errors.Wrap(err, "WASI execution failed")
	}

	// Parse response
	var response WASIResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return false, errors.Wrap(err, "failed to parse WASI response")
	}

	if response.Status != "Success" {
		return false, errors.New(response.Message)
	}

	// Extract verification result
	var result struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return false, errors.Wrap(err, "failed to parse verification result")
	}

	return result.Valid, nil
}

// FilterCommand represents the WASI filter command
type FilterCommand struct {
	Cmd          string            `json:"cmd"`
	Attestations []WASIAttestation `json:"attestations"`
	Subject      *string           `json:"subject,omitempty"`
	Predicate    *string           `json:"predicate,omitempty"`
	Context      *string           `json:"context,omitempty"`
	Actor        *string           `json:"actor,omitempty"`
}

// Filter runs WASI filtering on multiple attestations
func (w *WASIRunner) Filter(ctx context.Context, attestations []types.As, filter *types.AxFilter) ([]types.As, error) {
	// Convert all attestations to WASI format
	wasiAtts := make([]WASIAttestation, len(attestations))
	for i, as := range attestations {
		wasiAtts[i] = ConvertToWASI(&as)
	}

	// Build filter command
	cmd := FilterCommand{
		Cmd:          "Filter",
		Attestations: wasiAtts,
	}

	// Apply filters
	if filter != nil {
		if len(filter.Subjects) > 0 {
			subject := filter.Subjects[0]
			cmd.Subject = &subject
		}
		if len(filter.Predicates) > 0 {
			predicate := filter.Predicates[0]
			cmd.Predicate = &predicate
		}
		if len(filter.Contexts) > 0 {
			context := filter.Contexts[0]
			cmd.Context = &context
		}
		if len(filter.Actors) > 0 {
			actor := filter.Actors[0]
			cmd.Actor = &actor
		}
	}

	// Marshal and execute
	input, err := json.Marshal(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal filter command")
	}

	output, err := w.executeWASI(ctx, input)
	if err != nil {
		return nil, errors.Wrap(err, "WASI filter execution failed")
	}

	// Parse response
	var response WASIResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, errors.Wrap(err, "failed to parse WASI response")
	}

	if response.Status != "Success" {
		return nil, errors.New(response.Message)
	}

	// Extract filtered attestations
	var result struct {
		Count        int               `json:"count"`
		Attestations []WASIAttestation `json:"attestations"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse filter result")
	}

	// Convert back to Go attestations (simplified - would need full conversion)
	filtered := make([]types.As, 0, result.Count)
	for _, wasiAtt := range result.Attestations {
		// Find matching attestation from original list
		for _, as := range attestations {
			if as.ID == wasiAtt.ID {
				filtered = append(filtered, as)
				break
			}
		}
	}

	return filtered, nil
}

// executeWASI runs the WASI module with given input and returns output
func (w *WASIRunner) executeWASI(ctx context.Context, input []byte) ([]byte, error) {
	// This is a simplified version - actual implementation would:
	// 1. Create module instance with stdin/stdout pipes
	// 2. Write input to stdin
	// 3. Execute the module
	// 4. Read output from stdout
	// 5. Return the output

	// For now, return a placeholder
	return nil, fmt.Errorf("executeWASI not fully implemented - requires wazero pipe setup")
}

// Close cleans up the WASI runtime
func (w *WASIRunner) Close() error {
	return w.runtime.Close(context.Background())
}
