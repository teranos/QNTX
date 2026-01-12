//go:build !cgo || !rustfuzzy

package server

// fuzzyBackendVersion returns "go" for the Go fallback implementation
func fuzzyBackendVersion() string {
	return "go"
}
