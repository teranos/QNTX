//go:build cgo && rustduckdb

package commands

import (
	"context"
	"database/sql"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// operationalDBPath is where the parquet backend keeps the tables that are
// not attestations. The parquet location is a bucket or a directory of
// immutable files; neither is somewhere SQLite can hold a mutable row.
const operationalDBPath = "qntx-operational.db"

// sendInterval is how often a landing file sends what it holds to the record,
// and so the most a lost host loses. A crashed process loses nothing: the
// rows wait in the landing file for the next send.
//
// "let's go for 6h"
const sendInterval = 6 * time.Hour

// unsentInterval is how often the count of what a landing file has not yet
// sent goes to Sentry, so the climb between sends is seen and not only its top.
const unsentInterval = time.Minute

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
	// the namespace's own operational file, and is sent from there (ADR-037).
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
	atsStore := storage.NewAtsStore(defaultLanding, logger.Logger, duckdbcgo.NamespaceDefault)

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
	systemStore := storage.NewAtsStore(systemLanding, logger.Logger, duckdbcgo.NamespaceSystem)

	// Watchers live here too: a declaration is an object, a fire is a row in a
	// stream, and neither belongs in the operational SQLite above.
	watcherStore, err := duckdbcgo.NewWatcherStore(location, duckdbcgo.NamespaceDefault)
	if err != nil {
		unwindOperational(database, rustStore)
		return nil, nil, "", nil, errors.Wrapf(err, "failed to open watchers at %s", location)
	}

	// default and system send for the life of the process. What they hold
	// unsent when it ends is sent when they next open.
	sacred.Go("parquet.send."+duckdbcgo.NamespaceDefault, func() {
		sendEvery(context.Background(), defaultLanding, duckStore, duckdbcgo.NamespaceDefault, nil)
	})
	sacred.Go("parquet.send."+duckdbcgo.NamespaceSystem, func() {
		sendEvery(context.Background(), systemLanding, systemDuck, duckdbcgo.NamespaceSystem, nil)
	})
	// Watcher fires have no landing file; their buffer is still in memory.
	sacred.Go("parquet.periodicFlush", func() {
		flushWatcherFires(watcherStore, 5*time.Second)
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
		landings: map[string]*sqlitecgo.RustStore{
			duckdbcgo.NamespaceDefault: defaultLanding.RustStore,
			duckdbcgo.NamespaceSystem:  systemLanding.RustStore,
		},
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
	// landings is every open landing file by namespace, the two opened at boot
	// included, so the checkpoint pulse reaches each one's WAL.
	landings map[string]*sqlitecgo.RustStore
}

// opened is what closing a namespace has to reach: the flusher, and once its
// last flush is done, every store the namespace opened.
type opened struct {
	stop     context.CancelFunc
	flushed  <-chan struct{}
	duck     *duckdbcgo.DuckdbStore
	watchers *duckdbcgo.WatcherStore
	landing  *sqlitecgo.RustStore
}

// landed is a namespace's landing file: the buffer its writes land in, and
// the count of what it holds that the record does not have yet.
type landed struct {
	*sqlitecgo.RustStore
	name   string
	sent   storage.FileSentMark
	unsent atomic.Int64
}

// CreateAttestation lands the write and counts it as unsent. The record is
// not touched: the next send carries it (ADR-037).
func (l *landed) CreateAttestation(as *types.As) error {
	if err := l.RustStore.CreateAttestation(as); err != nil {
		return err
	}
	l.unsent.Add(1)
	return nil
}

// openLanding opens the file a namespace's attestations land in and are read
// from (ADR-037): one per namespace, beside the operational db, named by the
// slug. The operational db is one file for the node and its attestations
// table carries no namespace, so a namespace's rows go in a file of its own.
//
// What the file holds past its send mark is sent first: a process that ended
// before its last send left it there. Then the record is read once, from the
// take-in mark on, and what the file lacks is taken in; the record already
// has those rows, so the send mark moves past them. A record that does not
// answer is a namespace that cannot open: a file behind the record would
// answer reads with a hole in them.
//
// A file that has never sent counts everything it holds as sent. Rows a
// process wrote before this send existed and lost before its flush are in the
// file and not the record, and this does not look for them.
func openLanding(dbPath, name string, record *duckdbcgo.DuckdbStore) (*landed, error) {
	path := landingPath(dbPath, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, errors.Wrapf(err, "failed to make %s for the landing files", filepath.Dir(path))
	}
	store, err := sqlitecgo.NewFileStore(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the landing file of %s at %s", name, path)
	}
	landing := &landed{RustStore: store, name: name, sent: storage.FileSentMark{Path: path + ".sent"}}
	fail := func(err error) (*landed, error) {
		sqlclose.Log(store.Close(), logger.Logger, "the landing file of "+name)
		return nil, err
	}

	_, hasSent, err := landing.sent.Read()
	if err != nil {
		return fail(errors.Wrapf(err, "the landing file of %s could not read its send mark", name))
	}
	if hasSent {
		if err := sendAndCompact(landing, record); err != nil {
			return fail(errors.Wrapf(err, "the landing file of %s could not send what it held before it closed", name))
		}
		// What that send carried was landed by a process that is gone, and
		// never counted here. Nothing is unsent now.
		landing.unsent.Store(0)
	}

	started := time.Now()
	took, err := storage.TakeIn(landing.RustStore, record, storage.FileMark{Path: path + ".taken-in"})
	if err != nil {
		return fail(errors.Wrapf(err, "the landing file of %s could not take in its record", name))
	}
	elapsed := time.Since(started)
	logger.Logger.Infow("Namespace taken in from the record",
		"namespace", name,
		"file", path,
		"whole", took.Whole,
		"since", took.Since.UTC().Format(time.RFC3339Nano),
		"found", took.Found,
		"taken_in", took.TakenIn,
		"took", elapsed,
	)
	whole := measure.String(measure.AttrWhole, strconv.FormatBool(took.Whole))
	measure.Took(measure.StoreTakenIn, elapsed, measure.String(measure.AttrStore, name), whole)
	measure.Sized(measure.StoreTakenInRows, took.Found, measure.String(measure.AttrStore, name), whole)

	if !hasSent || took.TakenIn > 0 {
		if err := storage.MarkAllSent(landing.RustStore, landing.sent); err != nil {
			return fail(errors.Wrapf(err, "the landing file of %s could not mark its record's rows as sent", name))
		}
	}

	// A whole record taken in is a WAL the size of the record, until it is
	// checkpointed. Doing it here rather than at the next pulse.
	if took.TakenIn > 0 {
		busy, walPages, checkpointed, err := store.WALCheckpointTruncate()
		if err != nil {
			return fail(errors.Wrapf(err, "the landing file of %s did not checkpoint after its take-in", name))
		}
		logger.Logger.Infow("Landing file checkpointed after take-in",
			"namespace", name, "busy", busy, "wal_pages", walPages, "checkpointed_pages", checkpointed)
	}
	return landing, nil
}

// WALCheckpointTruncate checkpoints the operational db and every landing file.
// The pulse checkpoints one handle, and a parquet node keeps a WAL per
// namespace: a landing file nobody checkpoints grows without bound, and
// default.db-wal reached 680 MB on its first take-in.
func (h *parquetHandles) WALCheckpointTruncate() (busy, walPages, checkpointedPages int, err error) {
	busy, walPages, checkpointedPages, err = h.RustStore.WALCheckpointTruncate()
	if err != nil {
		return busy, walPages, checkpointedPages, err
	}
	h.mu.Lock()
	names := slices.Sorted(maps.Keys(h.landings))
	files := maps.Clone(h.landings)
	h.mu.Unlock()
	for _, name := range names {
		b, w, c, lerr := files[name].WALCheckpointTruncate()
		if lerr != nil {
			return busy, walPages, checkpointedPages, errors.Wrapf(lerr, "the landing file of %s did not checkpoint", name)
		}
		logger.Logger.Infow("Landing file checkpointed",
			"namespace", name, "busy", b, "wal_pages", w, "checkpointed_pages", c)
		busy += b
		walPages += w
		checkpointedPages += c
	}
	return busy, walPages, checkpointedPages, nil
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
		sqlclose.Log(duck.Close(), logger.Logger, "the parquet store of "+name)
		return nil, err
	}
	watchers, err := duckdbcgo.NewWatcherStore(h.location, name)
	if err != nil {
		sqlclose.Log(landing.Close(), logger.Logger, "the landing file of "+name)
		sqlclose.Log(duck.Close(), logger.Logger, "the parquet store of "+name)
		return nil, errors.Wrapf(err, "failed to open the watchers of %s at %s", name, h.location)
	}

	// Registered once everything opened, so a failed open leaves nothing running.
	// Landed rows reach Parquet on this send, the same as the stores opened at boot.
	ctx, stop := context.WithCancel(context.Background())
	flushed := make(chan struct{})
	h.mu.Lock()
	if h.closing == nil {
		h.closing = map[string]opened{}
	}
	h.closing[name] = opened{stop: stop, flushed: flushed, duck: duck, watchers: watchers, landing: landing.RustStore}
	h.landings[name] = landing.RustStore
	h.mu.Unlock()
	sacred.Go("parquet.send."+name, func() { sendEvery(ctx, landing, duck, name, flushed) })

	// Prompts are attestations of this namespace, so they land where its
	// other attestations do and are sent with them.
	store := storage.NewAtsStore(landing, logger.Logger, name)
	return namespaces.NewUniverse(name, namespaces.Made{
		Store:       store,
		Watchers:    duckdbcgo.NewWatchers(watchers),
		Schedules:   schedule.NewStore(h.operational),
		Canvas:      glyphstorage.NewCanvasStore(h.operational),
		Embeddings:  storage.NewEmbeddingStore(h.operational, logger.Logger.Desugar()),
		Rich:        storage.NewBoundedStore(h.operational, nil, logger.Logger),
		Executions:  schedule.NewExecutionStore(h.operational),
		Prompts:     prompt.NewPromptStore(h.operational, store),
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
	delete(h.landings, name)
	h.mu.Unlock()
	if !open {
		return
	}
	was.stop()
	// The last send, and the compaction it may start, finish before anything
	// closes, so a delete that follows drains what they wrote.
	<-was.flushed
	sqlclose.Log(was.watchers.Close(), logger.Logger, "the watchers of "+name)
	sqlclose.Log(was.duck.Close(), logger.Logger, "the parquet store of "+name)
	sqlclose.Log(was.landing.Close(), logger.Logger, "the landing file of "+name)
}

// EndNamespace removes the landing file of a namespace the store has ended,
// with its WAL, its mark and its flight records, so a namespace made later
// under the name starts empty (ADR-037).
func (h *parquetHandles) EndNamespace(name string) error {
	h.CloseNamespace(name)
	return removeLanding(h.dbPath, name)
}

// sendEvery sends a landing file's unsent rows to the record every
// sendInterval, and says how many are unsent every unsentInterval, until the
// namespace is closed. done, when there is one, closes once the last send is
// finished.
//
// The last send is on the way out. A namespace being switched off or deleted
// is not a namespace being told to lose writes, and a deleted one takes its
// landing file with it.
func sendEvery(ctx context.Context, landing *landed, record *duckdbcgo.DuckdbStore, name string, done chan<- struct{}) {
	if done != nil {
		defer close(done)
	}
	send := time.NewTicker(sendInterval)
	defer send.Stop()
	unsent := time.NewTicker(unsentInterval)
	defer unsent.Stop()
	// Per tick, not per goroutine: a namespace whose sender died keeps taking
	// attestations and sends none of them.
	sayUnsent := func() {
		measure.Gauge(measure.StoreUnsent, float64(landing.unsent.Load()),
			measure.String(measure.AttrStore, name))
	}
	sendOnce := func() {
		defer sacred.Said("parquet.send." + name)
		if err := sendAndCompact(landing, record); err != nil {
			logger.Logger.Errorw("Sending to the record failed; the rows wait in the landing file for the next send",
				"store", name, "error", err)
		}
		sayUnsent()
	}
	for {
		select {
		case <-ctx.Done():
			sendOnce()
			return
		case <-send.C:
			sendOnce()
		case <-unsent.C:
			sayUnsent()
		}
	}
}

// sendAndCompact sends what the landing file holds past its send mark, then
// asks for compaction (ADR-024), then says how many files the record holds.
//
// A send that wrote grows the file count, so it is the moment the threshold
// can have been crossed.
func sendAndCompact(landing *landed, record *duckdbcgo.DuckdbStore) error {
	name := landing.name
	store := measure.String(measure.AttrStore, name)

	started := time.Now()
	sent, err := storage.SendOut(landing, record, landing.sent)
	landing.unsent.Add(-int64(sent))
	if err != nil {
		return errors.Wrapf(err, "sent %d attestations of %s before the send failed", sent, name)
	}
	if sent == 0 {
		return nil
	}
	took := time.Since(started)
	logger.Logger.Infow("Sent to the record", "store", name, "rows", sent, "took", took)
	measure.Took(measure.StoreSent, took, store)
	measure.Sized(measure.StoreSentRows, sent, store)

	started = time.Now()
	files, bytes, err := record.Compact()
	if err != nil {
		return errors.Wrapf(err, "the record of %s did not compact", name)
	}
	if files > 0 {
		took := time.Since(started)
		logger.Logger.Infow("Compacted Parquet files", "store", name, "files", files, "bytes", bytes, "took", took)
		measure.Took(measure.StoreCompacted, took, store)
		measure.Sized(measure.StoreCompactedFiles, files, store)
		measure.Sized(measure.StoreCompactedBytes, int(bytes), store)
	}

	count, err := record.FileCount()
	if err != nil {
		return errors.Wrapf(err, "the record of %s did not say how many files it holds", name)
	}
	measure.Gauge(measure.StoreFiles, float64(count), store)
	return nil
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

func flushWatcherFires(watchers *duckdbcgo.WatcherStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		// Per tick, not per goroutine. This is the only thing that moves
		// buffered fires into Parquet, so a recover at the goroutine boundary
		// would log the panic and then leave the node recording fires that
		// reach the bucket never. One bad tick is one bad tick; the next one
		// still runs.
		func() {
			defer sacred.Said("parquet.flush.watcher_fires")
			if err := watchers.Flush(); err != nil {
				logger.Logger.Errorw("periodic watcher fire flush failed", "error", err)
			}
		}()
	}
}
