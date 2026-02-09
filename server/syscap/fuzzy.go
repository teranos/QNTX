package syscap

// fuzzyBackendVersion returns the version of the fuzzy matching backend.
// When WASM is available, returns the qntx-core version. Otherwise "go".
func fuzzyBackendVersion() string {
	return "go"
}
