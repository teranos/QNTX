package storage

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// RawAttestationStore is the minimum backend contract: bare CRUD, no domain
// logic. The concrete CGO stores for each backend (sqlitecgo.RustStore,
// duckdbcgo.DuckdbStore, ...) satisfy this by shape — Go's structural
// interfaces mean no explicit `implements` clauses.
//
// The shared Go-side wrapper AtsStore takes a RawAttestationStore and adds
// signing, observer notification, and vanity-ASID generation. Backend-specific
// concerns (enforcement, WAL checkpoint, backup, flush, Parquet-file layout,
// httpfs) stay on the concrete types, not this interface.
//
// Adding a third backend (Postgres, an object store, a plugin-provided
// gRPC store, ...) means writing a Go type that satisfies these methods —
// nothing more.
type RawAttestationStore interface {
	CreateAttestation(as *types.As) error
	GetAttestation(id string) (*types.As, error)
	AttestationExists(id string) bool
	CountAttestations() (int, error)
}

// QueryableStore is an optional extension for backends that support
// filter-based queries. AtsStore.GetAttestations delegates when the raw
// store satisfies this and returns an error otherwise.
type QueryableStore interface {
	GetAttestations(filter ats.AttestationFilter) ([]*types.As, error)
}
