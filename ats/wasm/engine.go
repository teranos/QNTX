// Package wasm provides a pure-Go bridge to qntx-core compiled as WebAssembly.
//
// The WASM module is embedded at build time and instantiated once on first use.
// All calls go through wazero (pure Go, no CGO). The module exposes functions
// from qntx-core (parser, fuzzy, classification) via shared memory.
//
// Memory protocol: strings cross the boundary as (ptr, len) pairs in WASM
// linear memory. Return values are packed as (ptr << 32) | len in a u64.
//
// Prerequisites: run `make rust-wasm` before `go build`.
// This compiles qntx-core to wasm32-unknown-unknown and copies the artifact here.
package wasm

//go:generate cargo build --release --target wasm32-unknown-unknown --package qntx-wasm --manifest-path ../../Cargo.toml
//go:generate cp ../../target/wasm32-unknown-unknown/release/qntx_wasm.wasm qntx_core.wasm

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

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
		return nil, fmt.Errorf("wasm compile: %w", err)
	}

	mod, err := r.InstantiateModule(ctx, compiled,
		wazero.NewModuleConfig().WithName("qntx-core"))
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("wasm instantiate: %w", err)
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
		return "", fmt.Errorf("wasm: missing export %q", fnName)
	}

	inputBytes := []byte(input)
	inputSize := uint64(len(inputBytes))

	var inputPtr uint64
	if inputSize > 0 {
		// Allocate space in WASM memory for the input
		results, err := allocFn.Call(ctx, inputSize)
		if err != nil {
			return "", fmt.Errorf("wasm alloc: %w", err)
		}
		inputPtr = results[0]
		if inputPtr == 0 {
			return "", fmt.Errorf("wasm alloc returned null")
		}

		// Write input bytes into WASM memory
		if !mod.Memory().Write(uint32(inputPtr), inputBytes) {
			freeFn.Call(ctx, inputPtr, inputSize)
			return "", fmt.Errorf("wasm memory write out of range")
		}
	}

	// Call the function
	results, err := targetFn.Call(ctx, inputPtr, inputSize)
	if err != nil {
		if inputSize > 0 {
			freeFn.Call(ctx, inputPtr, inputSize)
		}
		return "", fmt.Errorf("wasm call %s: %w", fnName, err)
	}

	// Free the input buffer
	if inputSize > 0 {
		freeFn.Call(ctx, inputPtr, inputSize)
	}

	// Unpack result: (ptr << 32) | len
	packed := results[0]
	resultPtr := uint32(packed >> 32)
	resultLen := uint32(packed & 0xFFFFFFFF)

	if resultPtr == 0 || resultLen == 0 {
		return "", fmt.Errorf("wasm %s returned null result", fnName)
	}

	// Read result from WASM memory
	resultBytes, ok := mod.Memory().Read(resultPtr, resultLen)
	if !ok {
		return "", fmt.Errorf("wasm memory read out of range")
	}

	// Copy before freeing (memory invalidated after free)
	output := make([]byte, len(resultBytes))
	copy(output, resultBytes)

	// Free the result buffer
	freeFn.Call(ctx, uint64(resultPtr), uint64(resultLen))

	return string(output), nil
}

// callNoArgsFn handles the shared-memory protocol for no-input, string-out
// WASM function calls (like version queries).
func callNoArgsFn(ctx context.Context, mod api.Module, fnName string) (string, error) {
	freeFn := mod.ExportedFunction("wasm_free")
	targetFn := mod.ExportedFunction(fnName)

	if freeFn == nil || targetFn == nil {
		return "", fmt.Errorf("wasm: missing export %q", fnName)
	}

	// Call the function with no arguments
	results, err := targetFn.Call(ctx)
	if err != nil {
		return "", fmt.Errorf("wasm call %s: %w", fnName, err)
	}

	// Unpack result: (ptr << 32) | len
	packed := results[0]
	resultPtr := uint32(packed >> 32)
	resultLen := uint32(packed & 0xFFFFFFFF)

	if resultPtr == 0 || resultLen == 0 {
		return "", fmt.Errorf("wasm %s returned null result", fnName)
	}

	// Read result from WASM memory
	resultBytes, ok := mod.Memory().Read(resultPtr, resultLen)
	if !ok {
		return "", fmt.Errorf("wasm memory read out of range")
	}

	// Copy before freeing (memory invalidated after free)
	output := make([]byte, len(resultBytes))
	copy(output, resultBytes)

	// Free the result buffer
	freeFn.Call(ctx, uint64(resultPtr), uint64(resultLen))

	return string(output), nil
}
