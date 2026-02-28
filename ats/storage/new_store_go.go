//go:build !cgo || !rustsqlite

package storage

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"go.uber.org/zap"
)

// NewStore returns a Go-backed attestation store (default path without Rust FFI).
func NewStore(db *sql.DB, dbPath string, logger *zap.SugaredLogger) (ats.AttestationStore, error) {
	return NewSQLStore(db, logger), nil
}
