package schedule

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// createTestDB creates an in-memory test database.
// Registers automatic cleanup via t.Cleanup().
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("Failed to enable foreign keys: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		db.Close()
	})

	return db
}
