package syscap

import (
	"github.com/teranos/QNTX/ats/ax"
)

// Get returns system capability information based on build configuration
// This detects available optimizations (Rust fuzzy matching, ONNX video inference, Rust storage)
func Get(fuzzyBackend ax.MatcherBackend) Message {
	// Detect fuzzy backend
	fuzzyOptimized := (fuzzyBackend == ax.MatcherBackendRust)
	fuzzyVersion := fuzzyBackendVersion()

	// Detect vidstream/ONNX availability (requires CGO build with rustvideo tag)
	vidstreamOptimized := vidstreamAvailable()
	vidstreamBackend := "onnx"
	vidstreamVersion := vidstreamBackendVersion()
	if !vidstreamOptimized {
		vidstreamBackend = "unavailable"
		vidstreamVersion = "n/a"
	}

	// Detect storage backend (requires CGO build with rustsqlite tag)
	storageOptimized := storageAvailable()
	storageBackend := "rust"
	storageVersion := storageBackendVersion()
	if !storageOptimized {
		storageBackend = "go"
	}

	return Message{
		Type:               "system_capabilities",
		FuzzyBackend:       string(fuzzyBackend),
		FuzzyOptimized:     fuzzyOptimized,
		FuzzyVersion:       fuzzyVersion,
		VidStreamBackend:   vidstreamBackend,
		VidStreamOptimized: vidstreamOptimized,
		VidStreamVersion:   vidstreamVersion,
		StorageBackend:     storageBackend,
		StorageOptimized:   storageOptimized,
		StorageVersion:     storageVersion,
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
