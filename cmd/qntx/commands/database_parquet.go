//go:build cgo && rustduckdb

package commands

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/namespaces"
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
	atsStore := storage.NewAtsStore(duckStore, logger.Logger, duckdbcgo.NamespaceDefault)

	// A node's own records — who was admitted, refused, released. system is a
	// node itself, so these belong to its store rather than a project's.
	systemDuck, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceSystem)
	if err != nil {
		database.Close()
		rustStore.Close()
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open the system store at %s", location)
	}
	systemStore := storage.NewAtsStore(systemDuck, logger.Logger, duckdbcgo.NamespaceSystem)

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
		RustStore:   rustStore,
		watchers:    duckdbcgo.NewWatchers(watcherStore),
		system:      systemStore,
		namespaces:  namespaces,
		location:    location,
		operational: database,
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
	// operational is where the tables that are not attestations still live
	// (ADR-024). A namespace is made of its schedules, and this is where they
	// are kept until the rows move under the namespace with everything else.
	operational *sql.DB
}

// OpenNamespace opens one namespace: its attestations and its watchers, which
// is what a namespace holds. The server asks the first time a request names one.
func (h *parquetHandles) OpenNamespace(name string) (*namespaces.Universe, error) {
	duck, err := duckdbcgo.NewDuckdbStore(h.location, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the %s store at %s", name, h.location)
	}
	// Buffered rows reach Parquet on this tick, the same as the two stores
	// opened at boot. Without it a write lives in memory until the process ends.
	go flushEvery(duck, name, 5*time.Second)

	watchers, err := duckdbcgo.NewWatcherStore(h.location, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the watchers of %s at %s", name, h.location)
	}

	return namespaces.NewUniverse(name, namespaces.Made{
		Store:       storage.NewAtsStore(duck, logger.Logger, name),
		Watchers:    duckdbcgo.NewWatchers(watchers),
		Schedules:   schedule.NewStore(h.operational),
		Canvas:      glyphstorage.NewCanvasStore(h.operational),
		Embeddings:  storage.NewEmbeddingStore(h.operational, logger.Logger.Desugar()),
		Rich:        storage.NewBoundedStore(h.operational, nil, logger.Logger),
		Executions:  schedule.NewExecutionStore(h.operational),
		Prompts:     prompt.NewPromptStore(h.operational, storage.NewAtsStore(duck, logger.Logger, name)),
		Aliases:     storage.NewAliasStore(h.operational),
		Queries:     storage.NewSQLQueryStore(h.operational),
		Operational: h.operational,
	})
}

// flushEvery writes a store's buffered attestations out on a tick.
func flushEvery(store *duckdbcgo.DuckdbStore, name string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		flushAndCompact(store, name)
	}
}

// flushAndCompact writes the buffer out, then asks for compaction (ADR-024).
//
// A flush that wrote grows the file count, so it is the moment the threshold
// can have been crossed. A merge rewrites the namespace, and the log line is
// how a human sees that it happened.
func flushAndCompact(store *duckdbcgo.DuckdbStore, name string) {
	rows, err := store.Flush()
	if err != nil {
		logger.Logger.Errorw("periodic parquet flush failed", "store", name, "error", err)
		return
	}
	if rows == 0 {
		return
	}
	logger.Logger.Debugw("Flushed to Parquet", "store", name, "rows", rows)

	started := time.Now()
	merged, err := store.Compact()
	if err != nil {
		logger.Logger.Errorw("parquet compaction failed", "store", name, "error", err)
		return
	}
	if merged > 0 {
		took := time.Since(started)
		logger.Logger.Infow("Compacted Parquet files", "store", name, "files", merged, "took", took)
		measure.Took(measure.StoreCompacted, took, measure.String(measure.AttrStore, name))
		measure.Sized(measure.StoreCompactedFiles, merged, measure.String(measure.AttrStore, name))
	}
}

// Namespaces is the capability namespace routes assert for.
func (h *parquetHandles) Namespaces() storage.Namespaces {
	return h.namespaces
}

// Universes is what this node holds: the default, system, the list of the rest,
// and the way to open one. A parquet node keeps namespaces, so it answers with
// all of it — dflt is the store already opened at boot, handed back rather than
// opened twice.
func (h *parquetHandles) Universes(dflt ats.AttestationStore) (*namespaces.Held, error) {
	made := namespaces.Made{
		Store:       dflt,
		Watchers:    h.watchers,
		Schedules:   schedule.NewStore(h.operational),
		Canvas:      glyphstorage.NewCanvasStore(h.operational),
		Embeddings:  storage.NewEmbeddingStore(h.operational, logger.Logger.Desugar()),
		Rich:        storage.NewBoundedStore(h.operational, nil, logger.Logger),
		Executions:  schedule.NewExecutionStore(h.operational),
		Prompts:     prompt.NewPromptStore(h.operational, dflt),
		Aliases:     storage.NewAliasStore(h.operational),
		Queries:     storage.NewSQLQueryStore(h.operational),
		Operational: h.operational,
	}
	def, err := namespaces.NewUniverse(duckdbcgo.NamespaceDefault, made)
	if err != nil {
		return nil, err
	}
	// system is the node itself, and it is made the same way anything is.
	made.Store = h.system
	sys, err := namespaces.NewUniverse(duckdbcgo.NamespaceSystem, made)
	if err != nil {
		return nil, err
	}

	held := &namespaces.Held{}
	held.SetDefault(def)
	held.SetSystem(sys)
	held.SetKnown(h.namespaces)
	held.SetOpener(h)
	return held, nil
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
		flushAndCompact(store, duckdbcgo.NamespaceDefault)
		flushAndCompact(system, duckdbcgo.NamespaceSystem)
		if err := watchers.Flush(); err != nil {
			logger.Logger.Errorw("periodic watcher fire flush failed", "error", err)
		}
	}
}
