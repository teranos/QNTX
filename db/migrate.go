package db

import (
	"database/sql"
	"embed"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/sym"
)

//go:embed sqlite/migrations/*.sql
var migrations embed.FS

// Migrate runs all pending migrations.
// If logger is provided, logs migration progress; otherwise operates silently.
func Migrate(db *sql.DB, logger *zap.SugaredLogger) error {
	// Read migration files
	entries, err := migrations.ReadDir("sqlite/migrations")
	if err != nil {
		return errors.Wrap(err, "read migrations")
	}

	// Sort migrations (000_create_schema_migrations.sql runs first)
	var migrationFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationFiles = append(migrationFiles, entry.Name())
		}
	}
	sort.Strings(migrationFiles)

	// Apply each migration
	for _, filename := range migrationFiles {
		version := strings.Split(filename, "_")[0]

		// Check if already applied (schema_migrations created by 000)
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
		if err != nil {
			// Table doesn't exist yet - this must be migration 000
			if version != "000" {
				return errors.Newf("schema_migrations table missing, but migration is not 000: %s", filename)
			}
		} else if exists {
			if logger != nil {
				logger.Debugw("Skipping migration (already applied)",
					"migration", filename,
					"version", version,
				)
			}
			continue
		}

		// Read and execute migration
		sqlBytes, err := migrations.ReadFile(filepath.Join("sqlite/migrations", filename))
		if err != nil {
			return errors.Wrapf(err, "read %s", filename)
		}

		if logger != nil {
			logger.Infow("Applying migration",
				"migration", filename,
				"version", version,
			)
		}

		tx, err := db.Begin()
		if err != nil {
			return errors.Wrapf(err, "begin tx for %s", filename)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "execute %s", filename)
		}

		// Record migration (000 creates the table, then records itself)
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "record %s", filename)
		}

		if err := tx.Commit(); err != nil {
			return errors.Wrapf(err, "commit %s", filename)
		}
	}

	if logger != nil {
		logger.Infow("Migrations complete",
			"symbol", sym.DB,
			"total_migrations", len(migrationFiles),
		)
	}

	return nil
}
