//go:build !cgo || !rustfuzzy

package syscap

// fuzzyBackendVersion returns "go" for the Go fallback implementation
func fuzzyBackendVersion() string {
	return "go"
}
