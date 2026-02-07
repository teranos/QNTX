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
