package syscap

// Message represents system capability information
// Sent once on WebSocket connection to inform client of available optimizations
type Message struct {
	Type               string `json:"type"`                // "system_capabilities"
	FuzzyBackend       string `json:"fuzzy_backend"`       // "rust" or "go" - which fuzzy matching implementation is active
	FuzzyOptimized     bool   `json:"fuzzy_optimized"`     // true if using Rust (optimized), false if Go fallback
	FuzzyVersion       string `json:"fuzzy_version"`       // fuzzy-ax library version (e.g., "0.1.0")
	VidStreamBackend   string `json:"vidstream_backend"`   // "onnx" or "unavailable" - video inference availability
	VidStreamOptimized bool   `json:"vidstream_optimized"` // true if ONNX Runtime available (CGO build)
	VidStreamVersion   string `json:"vidstream_version"`   // vidstream library version (e.g., "0.1.0")
}
