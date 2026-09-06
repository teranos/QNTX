package storage

// NamespaceDefinition is what a namespace's ns.toml says (ADR-026). The owner
// is an identity inside QNTX; the DID that proves you reach it is outside.
//
// The last five fields are the states a namespace passes through on its way out
// of service. A definition carrying none of them is one in service.
type NamespaceDefinition struct {
	Owner     string `json:"owner"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"` // RFC3339
	// DrainedInto is the namespace every attestation here was written into.
	// What is under this prefix stays under it — draining supersedes rather
	// than moves — and reads and writes are refused naming the target.
	DrainedInto string `json:"drained_into,omitempty"`
	DrainedAt   string `json:"drained_at,omitempty"` // RFC3339
	// DrainedCount is how many attestations were carried across. Deleting holds
	// it against what the prefix still answers with, so empty is counted.
	DrainedCount int `json:"drained_count,omitempty"`
	// DeletedAt and DeletedBy say the namespace is gone. The file stays, so the
	// name cannot be taken again over the bytes the old one wrote.
	DeletedAt string `json:"deleted_at,omitempty"` // RFC3339
	DeletedBy string `json:"deleted_by,omitempty"`
}

// Drained reports whether this namespace was drained into another one.
func (d NamespaceDefinition) Drained() bool { return d.DrainedInto != "" }

// Deleted reports whether this namespace was deleted. Nothing opens it, and
// the name is not free — the definition that says so is still on the location.
func (d NamespaceDefinition) Deleted() bool { return d.DeletedAt != "" }

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
	// Amend supersedes what a namespace's ns.toml says, and is the whole of how
	// a namespace goes out of service: drained into another, and then deleted.
	// Nothing under the prefix is touched by it, ever.
	Amend(name string, definition NamespaceDefinition) error
}
