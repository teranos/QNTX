//go:build !qntxwasm

package syscap

// fuzzyBackendVersion returns "go" when WASM is not available.
func fuzzyBackendVersion() string {
	return "go"
}
