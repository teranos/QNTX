package testutil

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// SetupTestDB creates an in-memory SQLite database for testing
// Exported for use by external tests (e.g., integration tests)
func SetupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE attestations (
			id TEXT PRIMARY KEY,
			subjects JSON NOT NULL,
			predicates JSON NOT NULL,
			contexts JSON NOT NULL,
			actors JSON NOT NULL,
			timestamp DATETIME NOT NULL,
			source TEXT NOT NULL,
			attributes JSON,
			created_at DATETIME NOT NULL
		)
	`)
	require.NoError(t, err)

	return db
}

// SetupEmptyDB creates an in-memory SQLite database WITHOUT the attestations table
// Used for testing error handling when schema is missing
func SetupEmptyDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	return db
}
