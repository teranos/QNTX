package commands

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/logger"
)

var driverOnce sync.Once

// openDatabase creates a unified database setup: Rust owns the SQLite connection,
// Go's *sql.DB routes all SQL through Rust via the "rustsqlite" driver.
// Returns the sql.DB handle, the attestation store, and the resolved path.
func openDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, error) {
	// Determine database path
	if dbPath == "" {
		path, err := am.GetDatabasePath()
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to get database path: %w", err)
		}
		if path == "" {
			dbPath = "qntx.db"
		} else {
			dbPath = path
		}
	}

	// Create Rust store (runs all migrations, sets up WAL/FK/busy_timeout)
	rustStore, err := sqlitecgo.NewFileStore(dbPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create Rust store at %s: %w", dbPath, err)
	}

	// Register the Rust SQL driver (once per process)
	driverOnce.Do(func() {
		rustdriver.Register(rustStore.StorePtr(), rustStore.Mu())
	})

	// Open *sql.DB through the Rust driver — single connection, no pooling
	database, err := sql.Open("rustsqlite", dbPath)
	if err != nil {
		rustStore.Close()
		return nil, nil, "", fmt.Errorf("failed to open rustsqlite driver: %w", err)
	}
	database.SetMaxOpenConns(1)

	// Create attestation store wrapping the Rust backend
	atsStore, err := storage.NewStoreFromRust(rustStore, logger.Logger)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", fmt.Errorf("failed to create attestation store: %w", err)
	}

	return database, atsStore, dbPath, nil
}
