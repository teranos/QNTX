package syscap

// Get returns system capability information based on build configuration.
// Fuzzy matching was removed — search will be provided by MeiliSearch
// via the qntx-meili plugin (ADR-015).
func Get() Message {
	// Detect storage backend (requires CGO build with rustsqlite tag)
	storageOptimized := storageAvailable()
	storageBackend := "rust"
	storageVersion := storageBackendVersion()
	if !storageOptimized {
		storageBackend = "go"
	}

	// Detect parser backend (requires qntxwasm build tag)
	parserOptimized := parserAvailable()
	parserBackend := "wasm"
	parserVersion := parserBackendVersion()
	parserSize := parserBackendSize()
	if !parserOptimized {
		parserBackend = "go"
		parserVersion = ""
		parserSize = ""
	}

	return Message{
		Type:             "system_capabilities",
		StorageBackend:   storageBackend,
		StorageOptimized: storageOptimized,
		StorageVersion:   storageVersion,
		ParserBackend:    parserBackend,
		ParserOptimized:  parserOptimized,
		ParserVersion:    parserVersion,
		ParserSize:       parserSize,
	}
}

// IsStorageOptimized returns true if using Rust SQLite backend
func IsStorageOptimized() bool {
	return storageAvailable()
}

// GetStorageVersion returns the storage backend version
func GetStorageVersion() string {
	return storageBackendVersion()
}
