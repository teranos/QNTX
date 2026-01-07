package testutil

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/db"
)

// SetupTestDB creates an in-memory SQLite database for testing.
// Uses real migrations to ensure test schema matches production schema.
// Automatically registers cleanup via t.Cleanup().
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Enable foreign keys
	_, err = testDB.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err, "Failed to enable foreign keys")

	// Apply real migrations (ensures test schema = production schema)
	err = db.Migrate(testDB, nil)
	require.NoError(t, err, "Failed to run migrations")

	// Register cleanup
	t.Cleanup(func() {
		testDB.Close()
	})

	return testDB
}

// SetupEmptyDB creates an in-memory SQLite database WITHOUT the attestations table
// Used for testing error handling when schema is missing
func SetupEmptyDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	return db
}
