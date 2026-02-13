package testing

import (
	"database/sql"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/db"
)

func init() {
	// Initialize sqlite-vec extension for vector similarity search in tests
	// This registers the vec0 module globally for all SQLite connections
	sqlite_vec.Auto()
}

// CreateTestDB creates an in-memory SQLite test database with migrations.
// Automatically registers cleanup via t.Cleanup().
func CreateTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create in-memory SQLite database
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Enable foreign keys
	if _, err := database.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("Failed to enable foreign keys: %v", err)
	}

	// Run migrations to set up schema (logger=nil for silent test migrations)
	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		database.Close()
	})

	return database
}
