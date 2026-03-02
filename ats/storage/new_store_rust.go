package storage

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// NewStore returns a Rust-backed attestation store with Go domain logic (signing, observers, bounded enforcement).
func NewStore(db *sql.DB, dbPath string, logger *zap.SugaredLogger) (ats.AttestationStore, error) {
	rustStore, err := sqlitecgo.NewFileStore(dbPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open Rust storage at %s", dbPath)
	}
	return &RustBackedStore{rust: rustStore, db: db, log: logger}, nil
}
