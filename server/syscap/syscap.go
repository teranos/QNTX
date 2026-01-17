package syscap

import (
	"github.com/teranos/QNTX/ats/ax"
)

// Get returns system capability information based on build configuration
// This detects available optimizations (Rust fuzzy matching, ONNX video inference)
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

	return Message{
		Type:               "system_capabilities",
		FuzzyBackend:       string(fuzzyBackend),
		FuzzyOptimized:     fuzzyOptimized,
		FuzzyVersion:       fuzzyVersion,
		VidStreamBackend:   vidstreamBackend,
		VidStreamOptimized: vidstreamOptimized,
		VidStreamVersion:   vidstreamVersion,
	}
}
