package db

import (
	"fmt"
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
		assert.Error(t, err)
		assert.Nil(t, db)

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
		// Create a scenario where WAL mode enabling will fail
		// 1. Create a temporary directory with a database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// 2. Create the database file first (so sql.Open succeeds)
		firstDB, err := Open(dbPath, nil)
		require.NoError(t, err)
		firstDB.Close()

		// 3. Make directory read-only so WAL files (.db-wal, .db-shm) can't be created
		err = os.Chmod(tmpDir, 0555) // r-x r-x r-x (no write)
		require.NoError(t, err)
		defer os.Chmod(tmpDir, 0755) // Restore for cleanup

		// 4. Try to open again - sql.Open succeeds (file exists) but WAL pragma fails
		db, err := Open(dbPath, nil)
		require.Error(t, err)
		require.Nil(t, db)

		// Verify error has stack trace from errors.Wrap
		stackTrace := errors.GetReportableStackTrace(err)
		require.NotNil(t, stackTrace, "errors from Open should have stack traces")

		// Verify detailed formatting includes stack trace with our wrapped context
		detailed := fmt.Sprintf("%+v", err)
		assert.Contains(t, detailed, "connection.go", "stack trace should reference source file")
		assert.Contains(t, detailed, "stack trace:", "detailed format should show stack trace section")
		assert.Contains(t, detailed, "db.Open", "stack should show db.Open function")

		// Verify we wrapped the error - it fails at WAL mode setup
		assert.Contains(t, detailed, "failed to enable WAL mode", "error should include our wrapped context")
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
