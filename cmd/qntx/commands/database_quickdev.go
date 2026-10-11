//go:build cgo && quickdev

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// openSqliteDatabase is QuickDev's SQLite, without ATS: migrated by Go's
// db.Migrate as the tests migrate it, and its attestations kept by
// storage.SQLRawStore. No Rust is linked, so there is no handle for the
// server's backend-specific hooks.
func openSqliteDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	dbPath, err := sqlitePath(dbPath)
	if err != nil {
		return nil, nil, "", nil, err
	}
	database, err := db.OpenWithMigrations(dbPath, logger.Logger)
	if err != nil {
		return nil, nil, "", nil, errors.Wrapf(err, "QuickDev did not open %s", dbPath)
	}
	logger.Logger.Warnw("This node is QuickDev: built without ATS or WASM, for developing against", "db", dbPath)
	return database, storage.NewAtsStore(storage.NewSQLRawStore(database), logger.Logger, auth.NamespaceDefault), dbPath, nil, nil
}
