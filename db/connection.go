package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/sym"
)

const (
	// SQLiteJournalMode configures the database journal mode (WAL enables concurrent reads)
	SQLiteJournalMode = "WAL"

	// SQLiteBusyTimeoutMS sets how long to wait for locks before returning SQLITE_BUSY
	SQLiteBusyTimeoutMS = 5000 // 5 seconds
)

// Open opens a SQLite database at the specified path with optimized settings.
// If logger is provided, logs database operations; otherwise operates silently.
func Open(path string, logger *zap.SugaredLogger) (*sql.DB, error) {
	if logger != nil {
		logger.Debugw("Opening database", "path", path, "symbol", sym.DB)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for concurrent reads during writes
	if _, err := db.Exec(fmt.Sprintf("PRAGMA journal_mode = %s", SQLiteJournalMode)); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign key constraints
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Set busy timeout
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", SQLiteBusyTimeoutMS)); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	if logger != nil {
		logger.Infow("Database opened successfully",
			"path", path,
			"symbol", sym.DB,
			"wal_mode", true,
			"foreign_keys", true,
		)
	}

	return db, nil
}
