//go:build cgo && !quickdev

package storage

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// NewStore returns a Rust-backed attestation store with Go domain logic (signing, observers).
func NewStore(dbPath string, logger *zap.SugaredLogger, namespace string) (ats.AttestationStore, error) {
	var rustStore *sqlitecgo.RustStore
	var err error
	if dbPath == ":memory:" {
		rustStore, err = sqlitecgo.NewMemoryStore()
	} else {
		rustStore, err = sqlitecgo.NewFileStore(dbPath)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open Rust storage at %s", dbPath)
	}
	runIntegrityCheck(rustStore, logger, dbPath)
	return &RustBackedStore{rust: rustStore, log: logger, namespace: namespace}, nil
}

// NewStoreFromRust wraps a pre-created RustStore with Go domain logic.
// Used when the RustStore is shared with the database/sql driver.
func NewStoreFromRust(rustStore *sqlitecgo.RustStore, logger *zap.SugaredLogger, namespace string) (ats.AttestationStore, error) {
	runIntegrityCheck(rustStore, logger, "")
	return &RustBackedStore{rust: rustStore, log: logger, namespace: namespace}, nil
}

// runIntegrityCheck runs PRAGMA integrity_check unless DEV mode is set.
// In dev mode the check is skipped — it takes 88s on a 1GB database.
func runIntegrityCheck(rustStore *sqlitecgo.RustStore, logger *zap.SugaredLogger, dbPath string) {
	if config.IsDevMode() {
		logger.Infow("Skipping SQLite integrity check (dev mode)", "db_path", dbPath)
		return
	}

	lines, err := rustStore.IntegrityCheck()
	if err != nil {
		logger.Errorw("SQLite integrity check failed to execute", "error", err, "db_path", dbPath)
	} else if len(lines) != 1 || lines[0] != "ok" {
		logger.Errorw("SQLite integrity check detected corruption — database may be damaged",
			"db_path", dbPath,
			"integrity_lines", lines,
		)
	} else {
		logger.Infow("SQLite integrity check passed", "db_path", dbPath)
	}
}
