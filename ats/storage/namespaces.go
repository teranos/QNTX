package storage

// NamespaceDefinition is what a namespace's ns.toml says (ADR-026). The owner
// is an identity inside QNTX; the DID that proves you reach it is outside.
type NamespaceDefinition struct {
	Owner     string          `json:"owner"`
	Enabled   bool            `json:"enabled"`
	CreatedAt string          `json:"created_at"` // RFC3339
	Record    NamespaceRecord `json:"record"`
}

// "what I want is for a Namespace to begin life dbless and enable either
// SQLite or parquet or Supabase later on"
//
// NamespaceRecord is a namespace's storage, as its ns.toml's [record] says.
// Every kind lands a write in the namespace's SQLite file first (ADR-037); the
// kind is what that file sends to.
type NamespaceRecord struct {
	Kind RecordKind `json:"kind"`
	// URL is Postgres's connection string as an ssm:// or env: reference,
	// never the password itself. Only a postgres record carries one.
	URL string `json:"url,omitempty"`
}

// RecordKind is what a namespace's SQLite file sends to.
type RecordKind string

const (
	// RecordNone is no storage. A namespace begins with none and is not
	// opened until it is given some.
	RecordNone RecordKind = "none"
	// RecordSQLite is the SQLite file alone, on the node that holds it.
	RecordSQLite RecordKind = "sqlite"
	// RecordParquet is Parquet at the node's location.
	RecordParquet RecordKind = "parquet"
	// RecordPostgres is Postgres, at the connection string URL names.
	RecordPostgres RecordKind = "postgres"
)

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
	// SetEnabled puts a namespace in or out of service. A disabled namespace
	// refuses reads, and re-enabling opens the same bytes again (ADR-027).
	SetEnabled(name string, enabled bool) error
	// SetRecord gives a namespace its storage. A namespace is given storage
	// once, from none.
	SetRecord(name string, record NamespaceRecord) error
	// Delete ends a namespace, draining what it held into default. What a
	// namespace holds outlives it; the namespace does not.
	Delete(name string) error
	// Nuke empties default without ending it. Everything a delete drains lands
	// there, so it is the one namespace that would otherwise only grow, and the
	// one place data leaves.
	Nuke() error
}
