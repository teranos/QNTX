//go:build cgo

package commands

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/errors"
)

var driverOnce sync.Once

// openDatabase creates a unified database setup: Rust owns the SQLite connection,
// Go's *sql.DB routes all SQL through Rust via the "rustsqlite" driver.
// Returns the sql.DB handle, the attestation store, the resolved path, and a
// WALCheckpointer for Rust-side WAL checkpoint (close readers, checkpoint, reopen).
func openDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	// Determine database path
	if dbPath == "" {
		path, err := am.GetDatabasePath()
		if err != nil {
			return nil, nil, "", nil, errors.Wrapf(err, "failed to get database path")
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
		return nil, nil, "", nil, errors.Wrapf(err, "failed to create Rust store at %s", dbPath)
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
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open rustsqlite driver")
	}
	database.SetMaxOpenConns(4)

	// Create attestation store wrapping the Rust backend
	atsStore, err := storage.NewStoreFromRust(rustStore, logger.Logger)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to create attestation store")
	}

	// Start mutex watchdog — only logs + dumps when there are actual waiters.
	// Write holder info is surfaced in the UI via live status; the watchdog
	// is now exclusively for diagnosing real contention (blocked goroutines).
	sqlitecgo.StartMutexWatchdog(rustStore.Mu(), sqlitecgo.WatchdogConfig{
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		OnAlert: func(blocked time.Duration) {
			holder, held := rustStore.WriteHolderInfo()
			if holder == "" {
				holder = "unknown"
			}

			buf := make([]byte, 64*1024)
			n := runtime.Stack(buf, true)
			dump := string(buf[:n])
			waiters := extractWaiters(dump)

			// No waiters = lock is held but nobody is blocked. Skip the log.
			if waiters == "none" {
				return
			}

			dir := "tmp/watchdog"
			os.MkdirAll(dir, 0755)
			filename := time.Now().Format("2006-01-02T15-04-05") + ".txt"
			path := filepath.Join(dir, filename)
			os.WriteFile(path, buf[:n], 0644)

			logger.Logger.Warnf("RustStore mutex contention — op: %s (held %s) — waiters: [%s] — dump: %s",
				holder, held.Truncate(time.Millisecond), waiters, path)
		},
	})

	return database, atsStore, dbPath, rustStore, nil
}

// extractWaiters scans a goroutine dump for goroutines blocked waiting on
// the write mutex or write queue result. Returns a comma-separated list of
// callers, e.g. "put:high, batch-put, watcher:evaluate".
func extractWaiters(dump string) string {
	blocks := strings.Split(dump, "\n\n")
	var waiters []string
	for _, block := range blocks {
		lines := strings.Split(block, "\n")
		// A waiter is a goroutine that contains "semacquire" (mutex contention)
		// or is blocked on chan receive after SubmitWrite.
		isWaiting := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "semacquire") || strings.Contains(trimmed, "SubmitWrite") {
				isWaiting = true
				break
			}
		}
		if !isWaiting {
			continue
		}
		// Find the deepest QNTX application frame to identify the caller
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "QNTX/") &&
				!strings.Contains(trimmed, "writequeue") &&
				!strings.Contains(trimmed, "watchdog") &&
				!strings.Contains(trimmed, "runtime") {
				if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
					caller := trimmed[idx+1:]
					if spaceIdx := strings.Index(caller, " "); spaceIdx > 0 {
						caller = caller[:spaceIdx]
					}
					waiters = append(waiters, caller)
				}
				break
			}
		}
	}
	if len(waiters) == 0 {
		return "none"
	}
	return strings.Join(waiters, ", ")
}
