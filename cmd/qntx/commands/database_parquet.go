//go:build cgo && rustduckdb

package commands

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/errors"
)

// openParquetDatabase builds the parquet-backed setup (ADR-024):
//   - Attestations go to a DuckDB store that flushes buffered rows to Parquet
//     files under `<location>/attestations/`.
//   - Operational Go-side tables (watchers, jobs, canvas, etc.) still speak to
//     a *sql.DB, backed here by an in-memory SQLite scratch. Everything that
//     runs against it starts empty and does not survive process restarts —
//     this is the "slowly port over" interim state, not the final shape.
//     Follow-up work moves each operational subsystem onto parquet-backed
//     stores and removes the scratch entirely.
func openParquetDatabase(cfg *config.Config) (*sql.DB, ats.AttestationStore, string, any, error) {
	location := cfg.Storage.Parquet.Location
	if location == "" {
		return nil, nil, "", nil, errors.New("storage.parquet.location is required when storage.backend = \"parquet\"")
	}

	// In-memory SQLite scratch for operational tables. Runs migrations, all
	// tables exist but empty. Attestations never land here.
	rustStore, err := sqlitecgo.NewMemoryStore()
	if err != nil {
		return nil, nil, "", nil, errors.Wrap(err, "failed to create scratch memory store for parquet backend")
	}
	driverOnce.Do(func() {
		rustdriver.Register(rustStore.StorePtr(), rustStore.ReadConnPtr(), rustStore.Mu(), rustStore.MuRead())
	})
	database, err := sql.Open("rustsqlite", ":memory:")
	if err != nil {
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrap(err, "failed to open rustsqlite scratch driver")
	}
	database.SetMaxOpenConns(4)

	// The parquet-backed attestation store — this is where attestations
	// actually land.
	duckStore, err := duckdbcgo.NewDuckdbStore(location)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open parquet store at %s", location)
	}
	atsStore := storage.NewAtsStore(duckStore, logger.Logger)

	// Periodic flush: writes buffered attestations to a new Parquet file
	// under `<location>/attestations/`. Rust also flushes from Drop as a
	// safety net, but Drop is not guaranteed on process termination.
	go periodicFlush(duckStore, 5*time.Second)

	// rustStore is returned as the opaque "extra" handle. It's the scratch
	// SQLite store — WAL checkpoint / age distiller assertions in server.go
	// will pick it up. Under parquet these do nothing meaningful (empty
	// tables), which is the intended degraded behavior for now.
	return database, atsStore, location, rustStore, nil
}

func periodicFlush(store *duckdbcgo.DuckdbStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := store.Flush(); err != nil {
			logger.Logger.Errorw("periodic parquet flush failed", "error", err)
		}
	}
}
