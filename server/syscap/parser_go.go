//go:build !qntxwasm

package syscap

// parserAvailable returns false when using native Go parser (no WASM)
func parserAvailable() bool {
	return false
}

// parserBackendVersion returns empty when not using WASM
func parserBackendVersion() string {
	return ""
}

// parserBackendSize returns empty when not using WASM
func parserBackendSize() string {
	return ""
}
