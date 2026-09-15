//go:build cgo && rustduckdb

package commands

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
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
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// operationalDBPath is where the parquet backend keeps the tables that are
// not attestations. The parquet location is a bucket or a directory of
// immutable files; neither is somewhere SQLite can hold a mutable row.
const operationalDBPath = "qntx-operational.db"

// unwindOperational closes what was already open when a later step failed.
//
// The failure being returned is why we are here, and a close that also failed
// has no other trace: the handle is gone, the process keeps running, and the
// file stays locked with nobody able to say by what.
func unwindOperational(database *sql.DB, rustStore *sqlitecgo.RustStore) {
	if database != nil {
		sqlclose.Log(database.Close(), logger.Logger, "the operational driver")
	}
	sqlclose.Log(rustStore.Close(), logger.Logger, "the operational store")
}

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
		unwindOperational(nil, rustStore)
		return nil, nil, "", nil, errors.Wrap(err, "failed to open the rustsqlite operational driver")
	}
	database.SetMaxOpenConns(4)

	// The parquet-backed attestation store is the record. A write lands in
	// the namespace's own operational file first (ADR-037).
	duckStore, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceDefault)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open parquet store at %s", location)
	}
	defaultLanding, err := openLanding(dbPath, duckdbcgo.NamespaceDefault, duckStore)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, err
	}
	atsStore := storage.NewAtsStore(storage.LandsFirst(defaultLanding, duckStore), logger.Logger, duckdbcgo.NamespaceDefault)

	// A node's own records — who was admitted, refused, released. system is a
	// node itself, so these belong to its store rather than a project's.
	systemDuck, err := duckdbcgo.NewDuckdbStore(location, duckdbcgo.NamespaceSystem)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open the system store at %s", location)
	}
	systemLanding, err := openLanding(dbPath, duckdbcgo.NamespaceSystem, systemDuck)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, err
	}
	systemStore := storage.NewAtsStore(storage.LandsFirst(systemLanding, systemDuck), logger.Logger, duckdbcgo.NamespaceSystem)

	// Watchers live here too: a declaration is an object, a fire is a row in a
	// stream, and neither belongs in the operational SQLite above.
	watcherStore, err := duckdbcgo.NewWatcherStore(location, duckdbcgo.NamespaceDefault)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open watchers at %s", location)
	}

	// Periodic flush: writes buffered attestations to a new Parquet file
	// under `<location>/attestations/`. Rust also flushes from Drop as a
	// safety net, but Drop is not guaranteed on process termination.
	sacred.Go("parquet.periodicFlush", func() {
		periodicFlush(duckStore, systemDuck, watcherStore, 5*time.Second)
	})

	// The extra handle carries capabilities server.go asserts for. It embeds
	// rustStore so the WAL checkpoint and age distiller assertions still find
	// what they were finding, and adds the watchers on top.
	// Spans every namespace at the location.
	namespaces, err := duckdbcgo.NewNamespaceStore(location)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open namespaces at %s", location)
	}

	extra := &parquetHandles{
		RustStore:   rustStore,
		watchers:    duckdbcgo.NewWatchers(watcherStore),
		system:      systemStore,
		namespaces:  namespaces,
		location:    location,
		dbPath:      dbPath,
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
	// dbPath is the operational db; each namespace's landing file sits beside it.
	dbPath string
	// operational is where the tables that are not attestations still live
	// (ADR-024). A namespace is made of its schedules, and this is where they
	// are kept until the rows move under the namespace with everything else.
	operational *sql.DB
	// closing is how each opened namespace is closed, by the name it was opened
	// under: its flusher stopped and its landing file closed. A namespace
	// switched off or deleted leaves a tick behind otherwise, on a prefix that
	// is not being served or is not there.
	mu      sync.Mutex
	closing map[string]opened
}

// opened is what closing a namespace has to reach.
type opened struct {
	stop    context.CancelFunc
	landing *sqlitecgo.RustStore
}

// openLanding opens the file a namespace's attestations land in first and are
// read from (ADR-037): one per namespace, beside the operational db, named by
// the slug. The operational db is one file for the node and its attestations
// table carries no namespace, so a namespace's rows go in a file of its own.
//
// The record is read once here, from the file's newest attestation on, and
// what the file lacks is taken in. A record that does not answer is a
// namespace that cannot open: a file behind the record would answer reads
// with a hole in them.
func openLanding(dbPath, name string, record storage.RawAttestationStore) (*sqlitecgo.RustStore, error) {
	dir := filepath.Join(filepath.Dir(dbPath), "namespaces")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, errors.Wrapf(err, "failed to make %s for the landing files", dir)
	}
	path := filepath.Join(dir, slug.Of(name)+".db")
	landing, err := sqlitecgo.NewFileStore(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the landing file of %s at %s", name, path)
	}
	started := time.Now()
	took, err := storage.TakeIn(landing, record, storage.FileMark{Path: path + ".taken-in"})
	if err != nil {
		sqlclose.Log(landing.Close(), logger.Logger, "the landing file of "+name)
		return nil, errors.Wrapf(err, "the landing file of %s could not take in its record", name)
	}
	logger.Logger.Infow("Namespace taken in from the record",
		"namespace", name,
		"file", path,
		"whole", took.Whole,
		"since", took.Since.UTC().Format(time.RFC3339Nano),
		"found", took.Found,
		"taken_in", took.TakenIn,
		"took", time.Since(started),
	)
	return landing, nil
}

// OpenNamespace opens one namespace: its attestations and its watchers, which
// is what a namespace holds. The server asks the first time a request names one.
func (h *parquetHandles) OpenNamespace(name string) (*namespaces.Universe, error) {
	duck, err := duckdbcgo.NewDuckdbStore(h.location, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the %s store at %s", name, h.location)
	}
	landing, err := openLanding(h.dbPath, name, duck)
	if err != nil {
		return nil, err
	}
	// Buffered rows reach Parquet on this tick, the same as the two stores
	// opened at boot. Without it a write lives in memory until the process ends.
	ctx, stop := context.WithCancel(context.Background())
	h.mu.Lock()
	if h.closing == nil {
		h.closing = map[string]opened{}
	}
	h.closing[name] = opened{stop: stop, landing: landing}
	h.mu.Unlock()
	sacred.Go("parquet.flushEvery."+name, func() { flushEvery(ctx, duck, name, 5*time.Second) })

	watchers, err := duckdbcgo.NewWatcherStore(h.location, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the watchers of %s at %s", name, h.location)
	}

	return namespaces.NewUniverse(name, namespaces.Made{
		Store:       storage.NewAtsStore(storage.LandsFirst(landing, duck), logger.Logger, name),
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

// CloseNamespace stops the flusher of a namespace that has been switched off or
// deleted, and waits for its last flush.
//
// Closing one nobody opened is the state this asks for, so it is not an error:
// a namespace can be switched off without anybody having reached it first.
func (h *parquetHandles) CloseNamespace(name string) {
	h.mu.Lock()
	was, open := h.closing[name]
	delete(h.closing, name)
	h.mu.Unlock()
	if !open {
		return
	}
	was.stop()
	sqlclose.Log(was.landing.Close(), logger.Logger, "the landing file of "+name)
}

// flushEvery writes a store's buffered attestations out on a tick, until the
// namespace is closed.
//
// The last flush is on the way out. A close that stopped at a tick boundary
// would drop whatever arrived since the previous one, and a namespace being
// switched off is not a namespace being told to lose writes.
func flushEvery(ctx context.Context, store *duckdbcgo.DuckdbStore, name string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			func() {
				defer sacred.Said("parquet.flush." + name)
				flushAndCompact(store, name)
			}()
			return
		case <-ticker.C:
			// Per tick, for the reason periodicFlush gives: a namespace whose
			// flusher died keeps taking attestations and keeps none of them.
			func() {
				defer sacred.Said("parquet.flush." + name)
				flushAndCompact(store, name)
			}()
		}
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
		// Per tick, not per goroutine. This is the only thing that moves
		// buffered rows into Parquet, so a recover at the goroutine boundary
		// would log the panic and then leave the node writing attestations
		// that reach the bucket never — accepting work it has quietly stopped
		// keeping. One bad tick is one bad tick; the next one still runs.
		func() {
			defer sacred.Said("parquet.flush." + duckdbcgo.NamespaceDefault)
			flushAndCompact(store, duckdbcgo.NamespaceDefault)
		}()
		func() {
			defer sacred.Said("parquet.flush." + duckdbcgo.NamespaceSystem)
			flushAndCompact(system, duckdbcgo.NamespaceSystem)
		}()
		func() {
			defer sacred.Said("parquet.flush.watcher_fires")
			if err := watchers.Flush(); err != nil {
				logger.Logger.Errorw("periodic watcher fire flush failed", "error", err)
			}
		}()
	}
}
