//go:build !cgo || !rustsqlite

package syscap

// storageAvailable returns false when falling back to Go SQLite implementation
func storageAvailable() bool {
	return false
}

// storageBackendVersion returns "go" for the Go fallback implementation
func storageBackendVersion() string {
	return "go"
}
