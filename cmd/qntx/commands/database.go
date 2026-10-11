//go:build cgo

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
)

// openDatabase dispatches to the backend-specific opener based on
// cfg.Storage.Backend (ADR-023). Each backend returns the same tuple:
// a *sql.DB for the operational Go-side tables, an ats.AttestationStore for
// attestation CRUD, the resolved path or location (for logging), and an
// opaque handle used by the server for backend-specific hooks
// (WALCheckpointer, AgeDistiller, etc.) via type assertions.
func openDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, "", nil, errors.Wrap(err, "failed to load config for openDatabase")
	}
	switch cfg.Storage.Backend {
	case "parquet":
		return openParquetDatabase(cfg, dbPath)
	case "sqlite", "":
		return openSqliteDatabase(dbPath)
	default:
		return nil, nil, "", nil, errors.Newf("unknown storage backend: %q", cfg.Storage.Backend)
	}
}

// sqlitePath is the SQLite file a node opens: the one asked for, else the
// configured one, else qntx.db.
func sqlitePath(dbPath string) (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	path, err := config.GetDatabasePath()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get database path")
	}
	if path == "" {
		return "qntx.db", nil
	}
	return path, nil
}
