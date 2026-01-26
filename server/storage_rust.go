//go:build rustsqlite
// +build rustsqlite

package server

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"go.uber.org/zap"
)

// createAttestationStore creates a Rust-backed attestation store
func createAttestationStore(db *sql.DB, dbPath string, logger *zap.SugaredLogger) (ats.AttestationStore, error) {
	logger.Infow("Using Rust SQLite storage backend", "path", dbPath)
	return sqlitecgo.NewFileStore(dbPath)
}
