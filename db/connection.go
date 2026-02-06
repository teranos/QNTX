package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
)

const (
	// SQLiteJournalMode configures the database journal mode (WAL enables concurrent reads)
	SQLiteJournalMode = "WAL"

	// SQLiteBusyTimeoutMS sets how long to wait for locks before returning SQLITE_BUSY
	SQLiteBusyTimeoutMS = 5000 // 5 seconds
)

// Open opens a SQLite database at the specified path with optimized settings.
// If log is provided, logs database operations; otherwise operates silently.
func Open(path string, log *zap.SugaredLogger) (*sql.DB, error) {
	if log != nil {
		logger.AddDBSymbol(log).Debugw("Opening database", "path", path)
	}

	// Ensure parent directory exists (SQLite can create file, but not directories)
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, errors.Wrapf(err, "failed to create database directory: %s", dir)
		}
		if log != nil {
			logger.AddDBSymbol(log).Debugw("Created database directory", "dir", dir)
		}
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open database at %s", path)
	}

	// Enable WAL mode for concurrent reads during writes
	if _, err := db.Exec("PRAGMA journal_mode = " + SQLiteJournalMode); err != nil {
		db.Close()
		return nil, errors.Wrapf(err, "failed to enable %s journal mode for %s", SQLiteJournalMode, path)
	}

	// Enable foreign key constraints
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, errors.Wrapf(err, "failed to enable foreign keys for %s", path)
	}

	// Set busy timeout
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, errors.Wrapf(err, "failed to set busy timeout to %dms for %s", SQLiteBusyTimeoutMS, path)
	}

	if log != nil {
		logger.AddDBSymbol(log).Infow("Database opened successfully",
			"path", path,
			"wal_mode", true,
			"foreign_keys", true,
		)
	}

	return db, nil
}

// OpenWithMigrations opens a SQLite database and runs migrations.
// This is a convenience function that combines Open() and Migrate().
// Migrations are idempotent and have low overhead for SQLite.
func OpenWithMigrations(path string, logger *zap.SugaredLogger) (*sql.DB, error) {
	db, err := Open(path, logger)
	if err != nil {
		return nil, err
	}

	if err := Migrate(db, logger); err != nil {
		db.Close()
		return nil, errors.Wrapf(err, "failed to run migrations for %s", path)
	}

	return db, nil
}
