// Package wasm provides a pure-Go bridge to qntx-core compiled as WebAssembly.
//
// The WASM module is embedded at build time and instantiated once on first use.
// All calls go through wazero (pure Go, no CGO). The module exposes functions
// from qntx-core (parser, fuzzy, classification) via shared memory.
//
// Memory protocol: strings cross the boundary as (ptr, len) pairs in WASM
// linear memory. Return values are packed as (ptr << 32) | len in a u64.
//
// Prerequisites: run `make wasm` before `go build`.
// This compiles qntx-core to wasm32-unknown-unknown and copies the artifact here.
package wasm

//go:generate cargo build --release --target wasm32-unknown-unknown --package qntx-wasm --manifest-path ../../Cargo.toml
//go:generate cp ../../target/wasm32-unknown-unknown/release/qntx_wasm.wasm qntx_core.wasm

import (
	"context"
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/teranos/QNTX/errors"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed qntx_core.wasm
var wasmBytes []byte

// Engine wraps a wazero runtime with a compiled qntx-core WASM module.
// A single module instance is reused for all calls (the exported functions
// are stateless pure functions). Access is serialized by a mutex.
type Engine struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	mod      api.Module

	mu sync.Mutex
}

var (
	globalEngine *Engine
	engineOnce   sync.Once
	engineErr    error
)

// GetEngine returns the singleton WASM engine, initializing it on first call.
func GetEngine() (*Engine, error) {
	engineOnce.Do(func() {
		globalEngine, engineErr = newEngine()
	})
	return globalEngine, engineErr
}

func newEngine() (*Engine, error) {
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)

	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		r.Close(ctx)
		return nil, errors.Wrap(err, "wasm compile")
	}

	mod, err := r.InstantiateModule(ctx, compiled,
		wazero.NewModuleConfig().WithName("qntx-core"))
	if err != nil {
		r.Close(ctx)
		return nil, errors.Wrap(err, "wasm instantiate")
	}

	return &Engine{
		runtime:  r,
		compiled: compiled,
		mod:      mod,
	}, nil
}

// Close releases all WASM resources.
func (e *Engine) Close() error {
	return e.runtime.Close(context.Background())
}

// Call invokes a named WASM function with a string input and returns the
// string output. The single module instance is reused across calls.
func (e *Engine) Call(fnName string, input string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return callStringFn(context.Background(), e.mod, fnName, input)
}

// CallNoArgs invokes a named WASM function with no input and returns the
// string output. Used for functions like qntx_core_version().
func (e *Engine) CallNoArgs(fnName string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return callNoArgsFn(context.Background(), e.mod, fnName)
}

// FuzzyMatch represents a single fuzzy match result from the WASM engine.
type FuzzyMatch struct {
	Value    string  `json:"value"`
	Score    float64 `json:"score"`
	Strategy string  `json:"strategy"`
}

// RebuildFuzzyIndex rebuilds the fuzzy engine's vocabulary index.
// Returns predicate count, context count, index hash, and any error.
func (e *Engine) RebuildFuzzyIndex(predicates, contexts []string) (int, int, string, error) {
	if predicates == nil {
		predicates = []string{}
	}
	if contexts == nil {
		contexts = []string{}
	}
	input, err := json.Marshal(struct {
		Predicates []string `json:"predicates"`
		Contexts   []string `json:"contexts"`
	}{Predicates: predicates, Contexts: contexts})
	if err != nil {
		return 0, 0, "", errors.Wrap(err, "marshal fuzzy_rebuild_index input")
	}

	raw, err := e.Call("fuzzy_rebuild_index", string(input))
	if err != nil {
		return 0, 0, "", err
	}

	var result struct {
		Predicates int    `json:"predicates"`
		Contexts   int    `json:"contexts"`
		Hash       string `json:"hash"`
		Error      string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return 0, 0, "", errors.Wrapf(err, "unmarshal fuzzy_rebuild_index result: %s", raw)
	}
	if result.Error != "" {
		return 0, 0, "", errors.Newf("fuzzy_rebuild_index: %s", result.Error)
	}

	return result.Predicates, result.Contexts, result.Hash, nil
}

// FindFuzzyMatches finds vocabulary items matching a query via the WASM fuzzy engine.
func (e *Engine) FindFuzzyMatches(query, vocabType string, limit int, minScore float64) ([]FuzzyMatch, error) {
	input, err := json.Marshal(struct {
		Query     string  `json:"query"`
		VocabType string  `json:"vocab_type"`
		Limit     int     `json:"limit"`
		MinScore  float64 `json:"min_score"`
	}{Query: query, VocabType: vocabType, Limit: limit, MinScore: minScore})
	if err != nil {
		return nil, errors.Wrap(err, "marshal fuzzy_find_matches input")
	}

	raw, err := e.Call("fuzzy_find_matches", string(input))
	if err != nil {
		return nil, err
	}

	// Check for error response
	var errResp struct {
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &errResp) == nil && errResp.Error != "" {
		return nil, errors.Newf("fuzzy_find_matches: %s", errResp.Error)
	}

	var matches []FuzzyMatch
	if err := json.Unmarshal([]byte(raw), &matches); err != nil {
		return nil, errors.Wrapf(err, "unmarshal fuzzy_find_matches result: %s", raw)
	}

	return matches, nil
}

// ClassifyClaimInput represents a single claim for WASM classification.
type ClassifyClaimInput struct {
	Subject     string `json:"subject"`
	Predicate   string `json:"predicate"`
	Context     string `json:"context"`
	Actor       string `json:"actor"`
	TimestampMs int64  `json:"timestamp_ms"`
	SourceID    string `json:"source_id"`
}

// ClassifyClaimGroup is a group of claims sharing the same key.
type ClassifyClaimGroup struct {
	Key    string               `json:"key"`
	Claims []ClassifyClaimInput `json:"claims"`
}

// ClassifyTemporalConfig holds configurable time windows (in milliseconds).
type ClassifyTemporalConfig struct {
	VerificationWindowMs int64 `json:"verification_window_ms"`
	EvolutionWindowMs    int64 `json:"evolution_window_ms"`
	ObsolescenceWindowMs int64 `json:"obsolescence_window_ms"`
}

// ClassifyInput is the full input for classify_claims.
type ClassifyInput struct {
	ClaimGroups []ClassifyClaimGroup `json:"claim_groups"`
	Config      ClassifyTemporalConfig `json:"config"`
	NowMs       int64                  `json:"now_ms"`
}

// ClassifyConflictOutput represents a single classified conflict.
type ClassifyConflictOutput struct {
	Subject         string              `json:"subject"`
	Predicate       string              `json:"predicate"`
	Context         string              `json:"context"`
	ConflictType    string              `json:"conflict_type"`
	Confidence      float64             `json:"confidence"`
	Strategy        string              `json:"strategy"`
	ActorHierarchy  []ClassifyActorRank `json:"actor_hierarchy"`
	TemporalPattern string              `json:"temporal_pattern"`
	AutoResolved    bool                `json:"auto_resolved"`
	SourceIDs       []string            `json:"source_ids"`
}

// ClassifyActorRank represents an actor with credibility ranking.
type ClassifyActorRank struct {
	Actor       string `json:"actor"`
	Credibility string `json:"credibility"`
	Timestamp   *int64 `json:"timestamp,omitempty"`
}

// ClassifyOutput is the result of classify_claims.
type ClassifyOutput struct {
	Conflicts      []ClassifyConflictOutput `json:"conflicts"`
	AutoResolved   int                      `json:"auto_resolved"`
	ReviewRequired int                      `json:"review_required"`
	TotalAnalyzed  int                      `json:"total_analyzed"`
}

// ClassifyClaims invokes the WASM classify_claims function.
func (e *Engine) ClassifyClaims(input ClassifyInput) (*ClassifyOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "marshal classify_claims input")
	}

	raw, err := e.Call("classify_claims", string(inputJSON))
	if err != nil {
		return nil, err
	}

	// Check for error response
	var errResp struct {
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &errResp) == nil && errResp.Error != "" {
		return nil, errors.Newf("classify_claims: %s", errResp.Error)
	}

	var output ClassifyOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, errors.Wrapf(err, "unmarshal classify_claims result: %s", raw)
	}

	return &output, nil
}

// ExpandAttestationInput represents a compact attestation for WASM cartesian expansion.
type ExpandAttestationInput struct {
	ID          string   `json:"id"`
	Subjects    []string `json:"subjects"`
	Predicates  []string `json:"predicates"`
	Contexts    []string `json:"contexts"`
	Actors      []string `json:"actors"`
	TimestampMs int64    `json:"timestamp_ms"`
}

// ExpandInput is the full input for expand_cartesian_claims.
type ExpandInput struct {
	Attestations []ExpandAttestationInput `json:"attestations"`
}

// ExpandClaimOutput represents a single expanded claim from the WASM engine.
type ExpandClaimOutput struct {
	Subject     string `json:"subject"`
	Predicate   string `json:"predicate"`
	Context     string `json:"context"`
	Actor       string `json:"actor"`
	TimestampMs int64  `json:"timestamp_ms"`
	SourceID    string `json:"source_id"`
}

// ExpandOutput is the result of expand_cartesian_claims.
type ExpandOutput struct {
	Claims []ExpandClaimOutput `json:"claims"`
	Total  int                 `json:"total"`
}

// ExpandCartesianClaims invokes the WASM expand_cartesian_claims function.
func (e *Engine) ExpandCartesianClaims(input ExpandInput) (*ExpandOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "marshal expand_cartesian_claims input")
	}

	raw, err := e.Call("expand_cartesian_claims", string(inputJSON))
	if err != nil {
		return nil, err
	}

	var errResp struct {
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &errResp) == nil && errResp.Error != "" {
		return nil, errors.Newf("expand_cartesian_claims: %s", errResp.Error)
	}

	var output ExpandOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, errors.Wrapf(err, "unmarshal expand_cartesian_claims result: %s", raw)
	}

	return &output, nil
}

// GroupClaimsInput is the input for group_claims.
type GroupClaimsInput struct {
	Claims []ExpandClaimOutput `json:"claims"`
}

// GroupClaimsGroupOutput represents a group of claims with the same key.
type GroupClaimsGroupOutput struct {
	Key    string              `json:"key"`
	Claims []ExpandClaimOutput `json:"claims"`
}

// GroupClaimsOutput is the result of group_claims.
type GroupClaimsOutput struct {
	Groups      []GroupClaimsGroupOutput `json:"groups"`
	TotalGroups int                      `json:"total_groups"`
}

// GroupClaims invokes the WASM group_claims function.
func (e *Engine) GroupClaims(input GroupClaimsInput) (*GroupClaimsOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "marshal group_claims input")
	}

	raw, err := e.Call("group_claims", string(inputJSON))
	if err != nil {
		return nil, err
	}

	var errResp struct {
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &errResp) == nil && errResp.Error != "" {
		return nil, errors.Newf("group_claims: %s", errResp.Error)
	}

	var output GroupClaimsOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, errors.Wrapf(err, "unmarshal group_claims result: %s", raw)
	}

	return &output, nil
}

// DedupInput is the input for dedup_source_ids.
type DedupInput struct {
	Claims []ExpandClaimOutput `json:"claims"`
}

// DedupOutput is the result of dedup_source_ids.
type DedupOutput struct {
	IDs   []string `json:"ids"`
	Total int      `json:"total"`
}

// DedupSourceIDs invokes the WASM dedup_source_ids function.
func (e *Engine) DedupSourceIDs(input DedupInput) (*DedupOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "marshal dedup_source_ids input")
	}

	raw, err := e.Call("dedup_source_ids", string(inputJSON))
	if err != nil {
		return nil, err
	}

	var errResp struct {
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &errResp) == nil && errResp.Error != "" {
		return nil, errors.Newf("dedup_source_ids: %s", errResp.Error)
	}

	var output DedupOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, errors.Wrapf(err, "unmarshal dedup_source_ids result: %s", raw)
	}

	return &output, nil
}

// GetWASMSize returns the size of the embedded WASM module in bytes.
func GetWASMSize() int {
	return len(wasmBytes)
}

// callStringFn handles the shared-memory protocol for string-in, string-out
// WASM function calls.
func callStringFn(ctx context.Context, mod api.Module, fnName string, input string) (string, error) {
	allocFn := mod.ExportedFunction("wasm_alloc")
	freeFn := mod.ExportedFunction("wasm_free")
	targetFn := mod.ExportedFunction(fnName)

	if allocFn == nil || freeFn == nil || targetFn == nil {
		return "", errors.Newf("wasm: missing export %q", fnName)
	}

	inputBytes := []byte(input)
	inputSize := uint64(len(inputBytes))

	var inputPtr uint64
	if inputSize > 0 {
		// Allocate space in WASM memory for the input
		results, err := allocFn.Call(ctx, inputSize)
		if err != nil {
			return "", errors.Wrapf(err, "wasm alloc for %s (size=%d)", fnName, inputSize)
		}
		inputPtr = results[0]
		if inputPtr == 0 {
			return "", errors.Newf("wasm alloc returned null for %s (size=%d)", fnName, inputSize)
		}

		// Write input bytes into WASM memory
		if !mod.Memory().Write(uint32(inputPtr), inputBytes) {
			// Best effort to free memory, but prioritize returning the write error
			if _, freeErr := freeFn.Call(ctx, inputPtr, inputSize); freeErr != nil {
				// Wrap both errors for debugging
				return "", errors.Wrapf(freeErr, "wasm %s memory write out of range at ptr=%d size=%d (also failed to free)", fnName, inputPtr, inputSize)
			}
			return "", errors.Newf("wasm %s memory write out of range at ptr=%d size=%d", fnName, inputPtr, inputSize)
		}
	}

	// Call the function
	results, err := targetFn.Call(ctx, inputPtr, inputSize)
	if err != nil {
		if inputSize > 0 {
			// Best effort to free memory on error path
			if _, freeErr := freeFn.Call(ctx, inputPtr, inputSize); freeErr != nil {
				return "", errors.Wrapf(err, "wasm call %s failed (also failed to free input at ptr=%d size=%d: %v)", fnName, inputPtr, inputSize, freeErr)
			}
		}
		return "", errors.Wrapf(err, "wasm call %s", fnName)
	}

	// Free the input buffer
	if inputSize > 0 {
		if _, err := freeFn.Call(ctx, inputPtr, inputSize); err != nil {
			// Input was processed but we failed to free memory - this is a leak
			return "", errors.Wrapf(err, "wasm %s memory leak: failed to free input buffer at ptr=%d size=%d", fnName, inputPtr, inputSize)
		}
	}

	// Unpack result: (ptr << 32) | len
	packed := results[0]
	resultPtr := uint32(packed >> 32)
	resultLen := uint32(packed & 0xFFFFFFFF)

	if resultPtr == 0 || resultLen == 0 {
		return "", errors.Newf("wasm %s returned null result (ptr=%d, len=%d)", fnName, resultPtr, resultLen)
	}

	// Read result from WASM memory
	resultBytes, ok := mod.Memory().Read(resultPtr, resultLen)
	if !ok {
		return "", errors.Newf("wasm %s memory read out of range at ptr=%d len=%d", fnName, resultPtr, resultLen)
	}

	// Copy before freeing (memory invalidated after free)
	output := make([]byte, len(resultBytes))
	copy(output, resultBytes)

	// Free the result buffer
	if _, err := freeFn.Call(ctx, uint64(resultPtr), uint64(resultLen)); err != nil {
		// Critical: failed to free WASM memory - this is a resource leak
		// We have the data, but leaking memory in WASM is unacceptable for a dev platform
		// that will be called repeatedly. Return error to force addressing the issue.
		return "", errors.Wrapf(err, "wasm %s memory leak: failed to free result buffer at ptr=%d size=%d", fnName, resultPtr, resultLen)
	}

	return string(output), nil
}

// callNoArgsFn handles the shared-memory protocol for no-input, string-out
// WASM function calls (like version queries).
func callNoArgsFn(ctx context.Context, mod api.Module, fnName string) (string, error) {
	freeFn := mod.ExportedFunction("wasm_free")
	targetFn := mod.ExportedFunction(fnName)

	if freeFn == nil || targetFn == nil {
		return "", errors.Newf("wasm: missing export %q", fnName)
	}

	// Call the function with no arguments
	results, err := targetFn.Call(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "wasm call %s", fnName)
	}

	// Unpack result: (ptr << 32) | len
	packed := results[0]
	resultPtr := uint32(packed >> 32)
	resultLen := uint32(packed & 0xFFFFFFFF)

	if resultPtr == 0 || resultLen == 0 {
		return "", errors.Newf("wasm %s returned null result (ptr=%d, len=%d)", fnName, resultPtr, resultLen)
	}

	// Read result from WASM memory
	resultBytes, ok := mod.Memory().Read(resultPtr, resultLen)
	if !ok {
		return "", errors.Newf("wasm %s memory read out of range at ptr=%d len=%d", fnName, resultPtr, resultLen)
	}

	// Copy before freeing (memory invalidated after free)
	output := make([]byte, len(resultBytes))
	copy(output, resultBytes)

	// Free the result buffer
	if _, err := freeFn.Call(ctx, uint64(resultPtr), uint64(resultLen)); err != nil {
		// Critical: failed to free WASM memory - this is a resource leak
		// We have the data, but leaking memory in WASM is unacceptable for a dev platform
		// that will be called repeatedly. Return error to force addressing the issue.
		return "", errors.Wrapf(err, "wasm %s memory leak: failed to free result buffer at ptr=%d size=%d", fnName, resultPtr, resultLen)
	}

	return string(output), nil
}
