//go:build !rustsqlite
// +build !rustsqlite

package server

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"go.uber.org/zap"
)

// createAttestationStore creates a Go SQL-backed attestation store
func createAttestationStore(db *sql.DB, dbPath string, logger *zap.SugaredLogger) (ats.AttestationStore, error) {
	return storage.NewSQLStore(db, logger), nil
}
