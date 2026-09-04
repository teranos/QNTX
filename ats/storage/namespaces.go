package storage

// NamespaceDefinition is what a namespace's ns.toml says (ADR-026). The owner
// is an identity inside QNTX; the DID that proves you reach it is outside.
type NamespaceDefinition struct {
	Owner     string `json:"owner"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"` // RFC3339
}

// Namespace is one namespace at a storage location. Definition is nil for the
// ones nobody wrote a ns.toml for — they are real, so they are listed.
type Namespace struct {
	Name       string               `json:"name"`
	Definition *NamespaceDefinition `json:"definition"`
	// Kinds is what it holds: attestations, watchers, schedules, tokens.
	Kinds []string `json:"kinds"`
}

// Namespaces is what the server needs of a backend that keeps namespaces, which
// is the seam a second backend fits through. Only parquet keeps them — a SQLite
// node has one universe, and does not implement this at all.
type Namespaces interface {
	List() ([]Namespace, error)
	Create(name string, definition NamespaceDefinition) error
}
