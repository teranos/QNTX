//go:build qntxwasm

package syscap

import (
	"fmt"

	"github.com/teranos/QNTX/ats/wasm"
)

// parserAvailable returns true when using qntx-core parser via WASM
func parserAvailable() bool {
	// Check if WASM engine can be initialized
	_, err := wasm.GetEngine()
	return err == nil
}

// parserBackendVersion returns the qntx-core version when using WASM
func parserBackendVersion() string {
	engine, err := wasm.GetEngine()
	if err != nil {
		return ""
	}

	version, err := engine.CallNoArgs("qntx_core_version")
	if err != nil {
		// Failed to get version, return empty
		return ""
	}

	return version
}

// parserBackendSize returns the WASM module size
func parserBackendSize() string {
	size := wasm.GetWASMSize()
	if size == 0 {
		return ""
	}

	// Format size in human-readable format
	const (
		KB = 1024
		MB = 1024 * KB
	)

	switch {
	case size >= MB:
		return fmt.Sprintf("%.1fMB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%dKB", size/KB)
	default:
		return fmt.Sprintf("%dB", size)
	}
}
