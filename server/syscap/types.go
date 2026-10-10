package syscap

// Message represents system capability information
// Sent once on WebSocket connection to inform client of available optimizations.
// Fuzzy fields removed — search will be provided by MeiliSearch (ADR-015).
type Message struct {
	Type string `json:"type"` // "system_capabilities"
	// Store is which store the node keeps (ADR-023) — "sqlite" or "parquet".
	// Distinct from StorageBackend, which is the implementation behind it.
	// Namespaces exist only under parquet, and sigma only under sqlite.
	Store            string `json:"store"`
	StorageBackend   string `json:"storage_backend"`   // "rust" or "go" - which storage implementation is active
	StorageOptimized bool   `json:"storage_optimized"` // true if using Rust SQLite (optimized), false if Go fallback
	StorageVersion   string `json:"storage_version"`   // ats-sqlite library version (e.g., "0.1.0")
	ParserBackend    string `json:"parser_backend"`    // "wasm" or "go" - which parser implementation is active
	ParserOptimized  bool   `json:"parser_optimized"`  // true if using ats via WASM, false if Go parser
	ParserVersion    string `json:"parser_version"`    // ats version when using WASM (e.g., "0.1.0")
	ParserSize       string `json:"parser_size"`       // WASM module size (e.g., "89KB")
}
