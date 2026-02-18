package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
)

// openDatabase opens and migrates a database using the specified path.
// If dbPath is empty, it loads from am config. Uses logger.Logger for db operations.
func openDatabase(dbPath string) (*sql.DB, error) {
	// Determine database path
	if dbPath == "" {
		path, err := am.GetDatabasePath()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get database path")
		}
		if path == "" {
			dbPath = "qntx.db"
		} else {
			dbPath = path
		}
	}

	// Open database with logger
	database, err := db.Open(dbPath, logger.Logger)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open database at %s", dbPath)
	}

	// Run migrations with logger
	if err := db.Migrate(database, logger.Logger); err != nil {
		database.Close()
		return nil, errors.Wrapf(err, "failed to run migrations on %s", dbPath)
	}

	return database, nil
}
