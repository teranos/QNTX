package db

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/sym"
	"github.com/teranos/errors"
)

//go:embed sqlite/migrations/*.sql
var migrations embed.FS

// checksumOf is what identifies a migration. The version is only its number,
// and a number can be applied by one branch and deleted from every other.
func checksumOf(sqlBytes []byte) string {
	sum := sha256.Sum256(sqlBytes)
	return hex.EncodeToString(sum[:])
}

// hasChecksumColumn reports whether schema_migrations can hold a checksum.
// False before migration 059 has run, where a mismatch cannot be detected.
func hasChecksumColumn(db *sql.DB) bool {
	rows, err := db.Query("PRAGMA table_info(schema_migrations)")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == "checksum" {
			return true
		}
	}
	return false
}

// sameMigrationAsRecorded refuses to continue when the file claiming a version
// is not the file that ran under it. A row with no checksum predates 059 and is
// backfilled, which is the only thing that can be said about it honestly.
func sameMigrationAsRecorded(db *sql.DB, version, filename, sum string, logger *zap.SugaredLogger) error {
	if !hasChecksumColumn(db) {
		return nil
	}

	var recorded sql.NullString
	if err := db.QueryRow(
		"SELECT checksum FROM schema_migrations WHERE version = ?", version,
	).Scan(&recorded); err != nil {
		return errors.Wrapf(err, "read the recorded checksum for %s", version)
	}

	if !recorded.Valid || recorded.String == "" {
		if _, err := db.Exec(
			"UPDATE schema_migrations SET checksum = ? WHERE version = ?", sum, version,
		); err != nil {
			return errors.Wrapf(err, "record the checksum for %s", version)
		}
		if logger != nil {
			logger.Debugw("Recorded the checksum of an already-applied migration",
				"migration", filename, "version", version)
		}
		return nil
	}

	if recorded.String == sum {
		return nil
	}

	// Skipping here is what let a withdrawn migration burn a number in silence.
	err := errors.Newf(
		"migration %s claims version %s, but a different migration was already applied under that version",
		filename, version)
	err = errors.WithDetail(err, "recorded checksum: "+recorded.String)
	err = errors.WithDetail(err, "this file's checksum: "+sum)
	err = errors.WithHint(err,
		"a version can be applied to a deployment and then deleted from every branch, so the "+
			"directory listing cannot tell you which numbers are free. Renumber this migration "+
			"above the highest version in the deployment's schema_migrations table.")
	return err
}

// Migrate runs all pending migrations.
// If logger is provided, logs migration progress; otherwise operates silently.
func Migrate(db *sql.DB, logger *zap.SugaredLogger) error {
	// Read migration files
	entries, err := migrations.ReadDir("sqlite/migrations")
	if err != nil {
		return errors.Wrap(err, "failed to read embedded migration files from sqlite/migrations")
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

		// Read first: the content is what says which migration this version is.
		sqlBytes, err := migrations.ReadFile(filepath.Join("sqlite/migrations", filename))
		if err != nil {
			return errors.Wrapf(err, "read %s", filename)
		}
		sum := checksumOf(sqlBytes)

		// Check if already applied (schema_migrations created by 000)
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
		if err != nil {
			// Table doesn't exist yet - this must be migration 000
			if version != "000" {
				return errors.Newf("schema_migrations table missing, but migration is not 000: %s", filename)
			}
		} else if exists {
			if err := sameMigrationAsRecorded(db, version, filename, sum, logger); err != nil {
				return err
			}
			if logger != nil {
				logger.Debugw("Skipping migration (already applied)",
					"migration", filename,
					"version", version,
				)
			}
			continue
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
			// A rollback that failed leaves this migration half-applied, and
			// the optional path below carries on to the next one regardless.
			// Reporting only the exec error would describe a schema that is
			// not the schema on disk.
			if rbErr := tx.Rollback(); rbErr != nil {
				return errors.Wrapf(err,
					"execute %s, and it could not be rolled back (%v), so the schema is part-applied",
					filename, rbErr)
			}
			// Migrations with "optional" in the name are allowed to fail —
			// they depend on extensions (e.g. sqlite-vec) that may not be loaded.
			if strings.Contains(filename, "optional") {
				if logger != nil {
					logger.Warnw("Optional migration skipped (extension not available)",
						"migration", filename,
						"error", err,
					)
				}
				continue
			}
			return errors.Wrapf(err, "execute %s", filename)
		}

		// Record migration (000 creates the table, then records itself). The
		// checksum goes with it once 059 has given the table somewhere to put
		// it; before that the version is all there is to record.
		record := "INSERT INTO schema_migrations (version) VALUES (?)"
		args := []any{version}
		if hasChecksumColumn(db) {
			record = "INSERT INTO schema_migrations (version, checksum) VALUES (?, ?)"
			args = append(args, sum)
		}
		if _, err := tx.Exec(record, args...); err != nil {
			// The migration ran and was not recorded, so it runs again next
			// boot. Whether the rollback undid it decides if that is safe.
			if rbErr := tx.Rollback(); rbErr != nil {
				return errors.Wrapf(err,
					"record %s, and it could not be rolled back (%v), so it is applied and unrecorded",
					filename, rbErr)
			}
			return errors.Wrapf(err, "record %s", filename)
		}

		if err := tx.Commit(); err != nil {
			return errors.Wrapf(err, "commit %s", filename)
		}
	}

	// 059 gives the table its checksum column part-way through this same pass,
	// so everything recorded before it still has none. Fill them in now rather
	// than leaving a boot where a mismatch would go unnoticed.
	if hasChecksumColumn(db) {
		for _, filename := range migrationFiles {
			version := strings.Split(filename, "_")[0]
			sqlBytes, err := migrations.ReadFile(filepath.Join("sqlite/migrations", filename))
			if err != nil {
				return errors.Wrapf(err, "read %s", filename)
			}
			if _, err := db.Exec(
				"UPDATE schema_migrations SET checksum = ? WHERE version = ? AND (checksum IS NULL OR checksum = '')",
				checksumOf(sqlBytes), version,
			); err != nil {
				return errors.Wrapf(err, "record the checksum for %s", version)
			}
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
