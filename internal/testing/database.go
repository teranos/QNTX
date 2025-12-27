package testing

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// CreateTestDB creates an in-memory SQLite test database.
// Automatically registers cleanup via t.Cleanup().
func CreateTestDB(t *testing.T) *sql.DB {
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
