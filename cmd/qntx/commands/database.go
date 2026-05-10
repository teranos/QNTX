//go:build cgo

package commands

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/internal/logger"
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

	// Start priority write queue — POST (high) jumps ahead of plugin writes (low).
	rustStore.StartWriteQueue(8, 64)

	// Register the Rust SQL driver (once per process)
	driverOnce.Do(func() {
		rustdriver.Register(rustStore.StorePtr(), rustStore.ReadConnPtr(), rustStore.Mu(), rustStore.MuRead())
	})

	// Open *sql.DB through the Rust driver.
	// MaxOpenConns(4) lets multiple goroutines reach the driver concurrently.
	// The driver's RustConn is stateless (Close is a no-op) — all connections
	// delegate to the same Rust store with muWrite/muRead mutex serialization.
	// With MaxOpenConns(1), reads and writes queue behind each other at the Go
	// pool layer even though the driver can handle them in parallel via separate
	// read/write connections (WAL mode). 4 slots eliminate the pool bottleneck.
	database, err := sql.Open("rustsqlite", dbPath)
	if err != nil {
		rustStore.Close()
		return nil, nil, "", fmt.Errorf("failed to open rustsqlite driver: %w", err)
	}
	database.SetMaxOpenConns(4)

	// Create attestation store wrapping the Rust backend
	atsStore, err := storage.NewStoreFromRust(rustStore, logger.Logger)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", fmt.Errorf("failed to create attestation store: %w", err)
	}

	// Start mutex watchdog — alerts when RustStore mutex is held too long.
	// Dumps all goroutine stacks to tmp/watchdog/ so the log stays scannable.
	sqlitecgo.StartMutexWatchdog(rustStore.Mu(), sqlitecgo.WatchdogConfig{
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		OnAlert: func(blocked time.Duration) {
			buf := make([]byte, 64*1024)
			n := runtime.Stack(buf, true)

			dir := "tmp/watchdog"
			os.MkdirAll(dir, 0755)
			filename := time.Now().Format("2006-01-02T15-04-05") + ".txt"
			path := filepath.Join(dir, filename)

			if err := os.WriteFile(path, buf[:n], 0644); err != nil {
				logger.Logger.Warnf("RustStore mutex held for %s — failed to write dump: %s", blocked, err)
				return
			}
			logger.Logger.Warnf("RustStore mutex held for %s — goroutine dump: %s", blocked, path)
		},
	})

	return database, atsStore, dbPath, nil
}
