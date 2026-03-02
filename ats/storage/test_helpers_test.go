package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db"
)

func init() {
	sqlite_vec.Auto()
}

// createTestStore creates a file-backed RustBackedStore for testing.
// Returns the store (for CRUD), the Go *sql.DB (for raw SQL queries and enforcement),
// and registers cleanup via t.Cleanup().
func createTestStore(t *testing.T) (ats.AttestationStore, *sql.DB) {
	t.Helper()

	// Create temp file for shared DB
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Open Go side
	goDb, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open Go test db: %v", err)
	}

	// Enable WAL mode for concurrent Go + Rust access
	if _, err := goDb.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("failed to enable WAL: %v", err)
	}
	if _, err := goDb.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	// Run migrations
	if err := db.Migrate(goDb, nil); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	// Open Rust side (same file)
	rustStore, err := sqlitecgo.NewFileStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create Rust store at %s: %v", dbPath, err)
	}

	store := &RustBackedStore{rust: rustStore, db: goDb, log: nil}

	t.Cleanup(func() {
		rustStore.Close()
		goDb.Close()
		os.Remove(dbPath)
	})

	return store, goDb
}

// createTestDB creates an in-memory SQLite test database with migrations.
// Use this for tests that only need raw SQL access (no store CRUD).
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	testDB.SetMaxOpenConns(1)

	if _, err := testDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	if err := db.Migrate(testDB, nil); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	t.Cleanup(func() {
		testDB.Close()
	})

	return testDB
}
