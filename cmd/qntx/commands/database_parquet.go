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

// operationalDBPath is where the parquet backend keeps the tables that are
// not attestations. The parquet location is a bucket or a directory of
// immutable files; neither is somewhere SQLite can hold a mutable row.
const operationalDBPath = "qntx-operational.db"

// openParquetDatabase builds the parquet-backed setup (ADR-024):
//   - Attestations go to a DuckDB store that flushes buffered rows to Parquet
//     files under `<location>/attestations/`.
//   - Operational Go-side tables (watchers, jobs, canvas, etc.) still speak to
//     a *sql.DB, backed here by SQLite on disk — this is the "slowly port
//     over" interim state, not the final shape. Follow-up work moves each
//     operational subsystem onto parquet-backed stores and removes it.
//
// The scratch used to be :memory:, which made every one of those tables
// truthful only until the process ended. A watcher is a standing instruction
// to react to something; one that a restart silently forgets is not a weaker
// watcher, it is a promise the system cannot keep. Plugins were hiding it —
// they re-declare their watchers and schedules at Initialize, so the loss was
// invisible for exactly the rows nobody outside a plugin had written.
func openParquetDatabase(cfg *config.Config, dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	location := cfg.Storage.Parquet.Location
	if location == "" {
		return nil, nil, "", nil, errors.New("storage.parquet.location is required when storage.backend = \"parquet\"")
	}

	if dbPath == "" {
		dbPath = operationalDBPath
	}

	// Operational tables on disk. Runs migrations; attestations never land here.
	rustStore, err := sqlitecgo.NewFileStore(dbPath)
	if err != nil {
		return nil, nil, "", nil, errors.Wrapf(err,
			"failed to open the operational store at %s for the parquet backend", dbPath)
	}
	driverOnce.Do(func() {
		rustdriver.Register(rustStore.StorePtr(), rustStore.ReadConnPtr(), rustStore.Mu(), rustStore.MuRead())
	})
	database, err := sql.Open("rustsqlite", dbPath)
	if err != nil {
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrap(err, "failed to open the rustsqlite operational driver")
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

	// rustStore is returned as the opaque "extra" handle — WAL checkpoint and
	// age distiller assertions in server.go pick it up. On disk these now act
	// on real rows rather than on an empty scratch.
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
