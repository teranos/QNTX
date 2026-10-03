//go:build cgo && atsless

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/errors"
)

// openSqliteDatabase is the ATSless node's SQLite: the operational tables,
// migrated by Go's db.Migrate as the tests migrate them, and no attestation
// store. No Rust is linked, so there is no handle for the server's
// backend-specific hooks.
func openSqliteDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	dbPath, err := sqlitePath(dbPath)
	if err != nil {
		return nil, nil, "", nil, err
	}
	database, err := db.OpenWithMigrations(dbPath, logger.Logger)
	if err != nil {
		return nil, nil, "", nil, errors.Wrapf(err, "the ATSless node did not open %s", dbPath)
	}
	logger.Logger.Warnw("This node runs ATSless: no attestation is written or read", "db", dbPath)
	return database, storage.Absent{}, dbPath, nil, nil
}
