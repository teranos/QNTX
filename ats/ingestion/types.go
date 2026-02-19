// Package ingestion provides interfaces for data ingestion.
// These types enable generic data producers to feed the attestation system
// without tight coupling to specific domain models.
package ingestion

// Item represents a triple (subject-predicate-context) from a data source.
// Data producers implement this interface to provide attestation-ready data.
type Item interface {
	GetSubject() string
	GetPredicate() string
	GetContext() string
	GetMeta() map[string]string
}

// Issue represents a warning or error from pipeline execution.
type Issue struct {
	Stage   string   `json:"stage"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Hints   []string `json:"hints,omitempty"`
}

// Stats captures execution metrics for pipeline operations.
type Stats struct {
	ReadCount    int   `json:"read_count"`
	ParsedCount  int   `json:"parsed_count"`
	WrittenCount int   `json:"written_count"`
	DurationMs   int64 `json:"duration_ms"`
}

// Result represents the structured execution result for data ingestion adapters.
// Implementations should provide Items (data), Warnings, Errors, and Stats.
type Result interface {
	GetItems() []Item
	GetWarnings() []Issue
	GetErrors() []Issue
	GetStats() Stats
}
