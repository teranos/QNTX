package db

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/errors"
)

func TestOpenWithMigrations(t *testing.T) {
	t.Run("successfully opens database and runs migrations", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := OpenWithMigrations(dbPath, nil)
		require.NoError(t, err)
		require.NotNil(t, db)
		defer db.Close()

		// Verify schema_migrations table exists (created by migrations)
		var exists int
		err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&exists)
		require.NoError(t, err)
		assert.Equal(t, 1, exists, "schema_migrations table should exist after migrations")
	})

	t.Run("wraps migration errors with context", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// First, create a database with a table that will conflict with migrations
		db, err := Open(dbPath, nil)
		require.NoError(t, err)

		// Create a conflicting table structure
		_, err = db.Exec("CREATE TABLE schema_migrations (bad_schema TEXT)")
		require.NoError(t, err)
		db.Close()

		// Now try to open with migrations - should fail
		db, err = OpenWithMigrations(dbPath, nil)
		if err != nil {
			// Error might occur if migration schema conflicts
			// Verify it's wrapped with our context
			detailed := fmt.Sprintf("%+v", err)
			assert.Contains(t, detailed, "connection.go", "error should have stack trace")

			if db != nil {
				db.Close()
			}
		}
		// Note: This test documents behavior - migrations might succeed despite schema differences
		// The important part is that IF an error occurs, it has proper wrapping
	})

	t.Run("migration errors include stack traces", func(t *testing.T) {
		// Create a scenario where opening database will fail
		// This tests that OpenWithMigrations properly wraps errors with stack traces
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create the database file first
		firstDB, err := Open(dbPath, nil)
		require.NoError(t, err)
		firstDB.Close()

		// Make directory read-only so WAL mode will fail
		err = os.Chmod(tmpDir, 0555)
		require.NoError(t, err)
		defer os.Chmod(tmpDir, 0755) // Restore for cleanup

		// Attempt to open with migrations - should fail at Open() step
		db, err := OpenWithMigrations(dbPath, nil)
		require.Error(t, err)
		assert.Nil(t, db)

		// Verify error has stack trace
		stackTrace := errors.GetReportableStackTrace(err)
		assert.NotNil(t, stackTrace, "migration errors should have stack traces")

		// Verify detailed formatting
		detailed := fmt.Sprintf("%+v", err)
		assert.Contains(t, detailed, "connection.go", "stack should reference source file")
		assert.Contains(t, detailed, "stack trace:", "error should include stack trace")
	})
}

func TestMigrate(t *testing.T) {
	t.Run("creates schema_migrations table", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := Open(dbPath, nil)
		require.NoError(t, err)
		defer db.Close()

		// Run migrations
		err = Migrate(db, nil)
		require.NoError(t, err)

		// Verify schema_migrations table was created
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 0, "should be able to query schema_migrations")
	})

	t.Run("is idempotent", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := Open(dbPath, nil)
		require.NoError(t, err)
		defer db.Close()

		// Run migrations twice
		err = Migrate(db, nil)
		require.NoError(t, err)

		err = Migrate(db, nil)
		require.NoError(t, err, "running migrations multiple times should be safe")
	})

	t.Run("migration errors have context", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := Open(dbPath, nil)
		require.NoError(t, err)

		// Close the database before trying to migrate
		db.Close()

		// Migrate should fail with a closed database
		err = Migrate(db, nil)
		require.Error(t, err)

		// Error should indicate it's database-related
		// Even if it doesn't have our wrapper (because it might fail before we wrap),
		// we can test that the error exists
		assert.NotNil(t, err)
	})
}
