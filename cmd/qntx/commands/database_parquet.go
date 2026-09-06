//go:build cgo && rustduckdb

package commands

import (
	"database/sql"
	"sync"
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
	duckStore, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceDefault)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open parquet store at %s", location)
	}
	atsStore := storage.NewAtsStore(duckStore, logger.Logger)

	// A node's own records — who was admitted, refused, released. system is a
	// node itself, so these belong to its store rather than a project's.
	systemDuck, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceSystem)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open the system store at %s", location)
	}
	systemStore := storage.NewAtsStore(systemDuck, logger.Logger)

	// Watchers live here too: a declaration is an object, a fire is a row in a
	// stream, and neither belongs in the operational SQLite above.
	watcherStore, err := duckdbcgo.NewWatcherStore(location, duckdbcgo.NamespaceDefault)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open watchers at %s", location)
	}

	// Periodic flush: writes buffered attestations to a new Parquet file
	// under `<location>/attestations/`. Rust also flushes from Drop as a
	// safety net, but Drop is not guaranteed on process termination.
	go periodicFlush(duckStore, systemDuck, watcherStore, 5*time.Second)

	// The extra handle carries capabilities server.go asserts for. It embeds
	// rustStore so the WAL checkpoint and age distiller assertions still find
	// what they were finding, and adds the watchers on top.
	// Spans every namespace at the location.
	namespaces, err := duckdbcgo.NewNamespaceStore(location)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open namespaces at %s", location)
	}

	extra := &parquetHandles{
		RustStore:  rustStore,
		watchers:   duckdbcgo.NewWatchers(watcherStore),
		system:     systemStore,
		namespaces: namespaces,
		location:   location,
	}
	// dbPath, not location: the caller hands this to NewQNTXServer as s.dbPath,
	// and everything reading it stats a file beside it. An s3:// URI there makes
	// every one of those stats fail quietly.
	return database, atsStore, dbPath, extra, nil
}

// parquetHandles is what a parquet node hands the server: the operational
// store it already expected, plus the parquet-backed watchers.
type parquetHandles struct {
	*sqlitecgo.RustStore
	watchers   *duckdbcgo.Watchers
	system     ats.AttestationStore
	namespaces storage.Namespaces
	location   string

	// What OpenNamespace has opened, so CloseNamespace has something to close.
	// Keyed by the name the store was opened under.
	mu     sync.Mutex
	opened map[string]*heldNamespace
}

// heldNamespace is one namespace this process is holding open: the handle, and
// the way to stop the loop that keeps flushing it.
type heldNamespace struct {
	duck *duckdbcgo.DuckdbStore
	stop chan struct{}
}

// OpenNamespace opens the attestation store for a namespace the server was not
// started with. The server asks the first time a request names one.
func (h *parquetHandles) OpenNamespace(name string) (ats.AttestationStore, error) {
	duck, err := duckdbcgo.NewDuckdbStore(h.location, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the %s store at %s", name, h.location)
	}
	held := &heldNamespace{duck: duck, stop: make(chan struct{})}

	h.mu.Lock()
	if h.opened == nil {
		h.opened = map[string]*heldNamespace{}
	}
	h.opened[name] = held
	h.mu.Unlock()

	// Buffered rows reach Parquet on this tick, the same as the two stores
	// opened at boot. Without it a write lives in memory until the process ends.
	go flushEvery(name, held, 5*time.Second)
	return storage.NewAtsStore(duck, logger.Logger), nil
}

// CloseNamespace stops the flush loop and releases the handle.
//
// The buffer is flushed once more on the way out: rows written in the last tick
// are still in memory, and dropping the handle without them would lose writes
// this node accepted. What is on disk is not touched — a prefix at the location
// outlives every process that opened it.
//
// A namespace this process never opened is not an error, because nothing is
// being held and there is nothing to say about it.
func (h *parquetHandles) CloseNamespace(name string) error {
	h.mu.Lock()
	held, open := h.opened[name]
	delete(h.opened, name)
	h.mu.Unlock()

	if !open {
		return nil
	}
	close(held.stop)
	if err := held.duck.Flush(); err != nil {
		// Said and not returned: the handle comes off either way. A flush that
		// failed with the store still open would leave the loop stopped and
		// nothing left to run it again.
		logger.Logger.Errorw("the last flush before closing a namespace failed",
			"namespace", name, "location", h.location, "error", err)
	}
	if err := held.duck.Close(); err != nil {
		return errors.Wrapf(err, "failed to close the %s store at %s", name, h.location)
	}
	return nil
}

// flushEvery writes a namespace's buffered attestations out on a tick, until
// the namespace is closed. The name is here so a failing flush says which
// namespace stopped reaching the location.
func flushEvery(name string, held *heldNamespace, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-held.stop:
			return
		case <-ticker.C:
			if err := held.duck.Flush(); err != nil {
				logger.Logger.Errorw("periodic parquet flush failed",
					"namespace", name, "error", err)
			}
		}
	}
}

// Namespaces is the capability namespace routes assert for.
func (h *parquetHandles) Namespaces() storage.Namespaces {
	return h.namespaces
}

// SystemStore is where the node writes about itself, separate from any project.
func (h *parquetHandles) SystemStore() ats.AttestationStore {
	return h.system
}

// Watchers is the capability server.go asserts for.
func (h *parquetHandles) Watchers() storage.Watchers {
	return h.watchers
}

func periodicFlush(
	store *duckdbcgo.DuckdbStore,
	system *duckdbcgo.DuckdbStore,
	watchers *duckdbcgo.WatcherStore,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := store.Flush(); err != nil {
			logger.Logger.Errorw("periodic parquet flush failed", "error", err)
		}
		if err := system.Flush(); err != nil {
			logger.Logger.Errorw("periodic system flush failed", "error", err)
		}
		if err := watchers.Flush(); err != nil {
			logger.Logger.Errorw("periodic watcher fire flush failed", "error", err)
		}
	}
}
