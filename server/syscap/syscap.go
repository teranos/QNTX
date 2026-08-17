package syscap

// Get returns system capability information. store is the configured storage
// backend, which build tags cannot answer — it is a choice in am.toml.
func Get(store string) Message {
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
		Store:            store,
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
