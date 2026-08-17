package storage

// NamespaceOwner is who a namespace belongs to (ADR-026). No private key — a
// namespace is owned, not a signer.
type NamespaceOwner struct {
	// OwnerDID is the node that signed, MintedBy the root identity that asked.
	// Access tokens already carry this pair for the same reason.
	OwnerDID  string `json:"owner_did"`
	MintedBy  string `json:"minted_by"`
	CreatedAt string `json:"created_at"` // RFC3339
}

// Namespace is one namespace at a storage location. Owner is nil for the ones
// that predate ownership — they are real, so they are listed.
type Namespace struct {
	Name  string          `json:"name"`
	Owner *NamespaceOwner `json:"owner"`
	// Kinds is what it holds: attestations, watchers, schedules, tokens.
	Kinds []string `json:"kinds"`
}

// Namespaces is what the server needs of a backend that keeps namespaces, which
// is the seam a second backend fits through. Only parquet keeps them — a SQLite
// node has one universe, and does not implement this at all.
type Namespaces interface {
	List() ([]Namespace, error)
	Create(name string, owner NamespaceOwner) error
	Delete(name string) error
}
