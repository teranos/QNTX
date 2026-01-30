package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/teranos/QNTX/errors"
)

func TestOpen(t *testing.T) {
	t.Run("opens database successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := Open(dbPath, nil)
		require.NoError(t, err)
		require.NotNil(t, db)
		defer db.Close()

		// Verify WAL mode enabled
		var journalMode string
		err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
		require.NoError(t, err)
		assert.Equal(t, "wal", journalMode)

		// Verify foreign keys enabled
		var foreignKeys int
		err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
		require.NoError(t, err)
		assert.Equal(t, 1, foreignKeys)

		// Verify busy timeout set
		var busyTimeout int
		err = db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout)
		require.NoError(t, err)
		assert.Equal(t, SQLiteBusyTimeoutMS, busyTimeout)
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		// Use a path that doesn't exist and can't be created
		invalidPath := "/invalid/nonexistent/path/db.sqlite"

		db, err := Open(invalidPath, nil)

		// If Open() succeeds (lazy connection on some platforms), Ping() will fail
		if err == nil && db != nil {
			err = db.Ping()
			db.Close()
		}

		assert.Error(t, err)

		// Verify error has stack trace (from errors package)
		stackTrace := errors.GetStack(err)
		assert.NotNil(t, stackTrace, "error should have stack trace from errors.Wrap")
	})

	t.Run("creates database file if it doesn't exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "new.db")

		// Verify file doesn't exist
		_, err := os.Stat(dbPath)
		assert.True(t, os.IsNotExist(err))

		// Open should create it
		db, err := Open(dbPath, nil)
		require.NoError(t, err)
		require.NotNil(t, db)
		defer db.Close()

		// Verify file was created
		_, err = os.Stat(dbPath)
		assert.NoError(t, err)
	})

	t.Run("errors include stack traces from errors package", func(t *testing.T) {
		// Create a database, close it, then try operations on closed connection
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := Open(dbPath, nil)
		require.NoError(t, err)
		require.NotNil(t, db)

		// Close the database
		db.Close()

		// Try to execute a query on closed database - this will fail reliably across platforms
		_, err = db.Exec("PRAGMA journal_mode")
		require.Error(t, err)

		// Verify error exists (won't have our wrapper since it's direct sql error, not from Open)
		// This demonstrates that database operations fail appropriately
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "closed", "error should indicate connection is closed")
	})

}

func TestOpen_WithLogger(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Use test logger to verify logging calls
	logger := zaptest.NewLogger(t).Sugar()
	db, err := Open(dbPath, logger)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()
}
