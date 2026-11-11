package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"os"
	"sort"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/db/rustdriver"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/syscap"
	errors "github.com/teranos/sacred-error"
)

// WriteLockInspector exposes write lock holder diagnostics.
// Implemented by RustStore.WriteHolderInfo.
type WriteLockInspector interface {
	WriteHolderInfo() (holder string, held time.Duration)
}

const dbStatsRefreshInterval = 30 * time.Second

// dbStatsPace is the multiple of a refresh's duration the refresher waits after
// it. At 10 the refresher is reading at most a tenth of the time.
const dbStatsPace = 10

// nextRefreshIn is the wait after a refresh: the interval, or ten times what
// the refresh took when that is longer.
func nextRefreshIn(took, interval time.Duration) time.Duration {
	return max(interval, took*dbStatsPace)
}

// paceRefreshes runs refresh, waits nextRefreshIn of what it says it took, and
// again, until ctx ends. One goroutine does both, so no refresh overlaps another.
func paceRefreshes(ctx context.Context, interval time.Duration, refresh func() (took time.Duration)) {
	for {
		took := refresh()
		select {
		case <-ctx.Done():
			return
		case <-time.After(nextRefreshIn(took, interval)):
		}
	}
}

// attestationCounter is a backend that counts the attestations it holds.
// RustStore and DuckdbStore both implement it.
type attestationCounter interface {
	CountAttestations() (int, error)
}

// RecordReporter is a backend whose record is somewhere the node has to ask,
// and which can say what the asking has cost. A backend holding everything on
// the node has nothing to report and does not implement this.
type RecordReporter interface {
	RecordSpend() ([]Spend, error)
}

// LandingReporter is a backend that answers reads from a database per
// namespace rather than from one (ADR-037). A backend keeping a single file
// does not implement it, and the panel then has one database to draw.
type LandingReporter interface {
	Landings() ([]Landing, error)
}

// A Landing is one namespace's database: where it is, how big it and its
// write-ahead log have grown, and how many attestations it answers from. Each
// is a stat or the backend's own count; nothing here scans a table.
type Landing struct {
	Namespace    string `json:"namespace"`
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	WalBytes     int64  `json:"wal_bytes"`
	Attestations int    `json:"attestations"`

	// CountTook is how long the backend took to count Attestations, which is
	// what the refresher names when a refresh overruns its interval.
	CountTook time.Duration `json:"-"`
}

// overAtMost is how many hourly buckets a rich field's line is drawn from, so
// a fortnight fits and the chart still has a shape.
const overAtMost = 336

// Spend is one reader and what it has cost against the record: the name make
// parity gives it, the request, and how many. HeldOnNode is filled by heldOnNode.
type Spend struct {
	Of         string `json:"of"`
	Request    string `json:"request"`
	HeldOnNode bool   `json:"held_on_node"`
	Count      int64  `json:"count"`
}

// spentAcross folds every reporter's tally into one row per reader and
// request, most spent first — the top row is where to look.
//
// A reporter that fails takes the whole answer down rather than being skipped:
// a total quietly missing one of its readers is worse than no total, because
// it reads as that reader spending nothing.
func spentAcross(reporters []RecordReporter) ([]Spend, error) {
	summed := map[Spend]int64{}
	for _, reporter := range reporters {
		spent, err := reporter.RecordSpend()
		if err != nil {
			return nil, err
		}
		for _, one := range spent {
			summed[Spend{Of: one.Of, Request: one.Request}] += one.Count
		}
	}

	spend := make([]Spend, 0, len(summed))
	for what, count := range summed {
		what.Count = count
		spend = append(spend, what)
	}
	sort.Slice(spend, func(i, j int) bool { return spend[i].Count > spend[j].Count })
	return spend, nil
}

// heldOnNode marks each reader the node keeps a table of. The answer is the
// node's own schema, which is what make parity reads for its SQLITE column,
// so the panel and parity read one source.
func heldOnNode(db *sql.DB, spend []Spend) (err error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return errors.Wrap(err, "the node's schema did not say which tables it holds")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for heldOnNode") }()

	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return errors.Wrap(err, "failed to scan a table name from sqlite_master")
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "the node's schema stopped answering")
	}

	// No table is no copy. A reader whose name matches nothing reads as
	// record only, never as held.
	for at := range spend {
		spend[at].HeldOnNode = tables[spend[at].Of]
	}
	return nil
}

// saySpend emits what each reader has cost since the last refresh.
//
// The stores count from when they opened and the metric takes a delta, so what
// was said last time is subtracted here rather than drained from the stores:
// each total has another reader in the panel, and draining would hand that one
// what this took. A restart is a gap and not a spike, because saidSpend starts
// empty beside stores that start at zero.
//
// Only refreshDBStats calls this, and only from the one goroutine
// startDBStatsRefresher runs, so saidSpend needs no lock.
func (s *QNTXServer) saySpend(spend []Spend) {
	if s.saidSpend == nil {
		s.saidSpend = map[Spend]int64{}
	}
	for _, one := range spend {
		seen := Spend{Of: one.Of, Request: one.Request}
		since := one.Count - s.saidSpend[seen]
		s.saidSpend[seen] = one.Count
		if since <= 0 {
			continue
		}
		measure.Count(measure.StoreRequests, since,
			measure.String(measure.AttrOf, one.Of),
			measure.String(measure.AttrRequest, one.Request))
	}
}

// rawUnwrapper is AtsStore's escape hatch to the concrete backend.
type rawUnwrapper interface {
	Raw() storage.RawAttestationStore
}

// ErrNoAttestationCounter is a store that cannot be asked for a count. A count
// that was asked for and failed is a different error.
var ErrNoAttestationCounter = errors.New("backend cannot count attestations")

// countAttestations asks the store that owns the attestations (ADR-024).
func countAttestations(store any) (int, error) {
	counter, ok := store.(attestationCounter)
	if !ok {
		unwrapped, isWrapper := store.(rawUnwrapper)
		if !isWrapper {
			return 0, errors.WithDetail(ErrNoAttestationCounter,
				fmt.Sprintf("store %T is neither a counter nor a wrapper around one", store))
		}
		if counter, ok = unwrapped.Raw().(attestationCounter); !ok {
			return 0, errors.WithDetail(ErrNoAttestationCounter,
				fmt.Sprintf("store %T wraps %T, which cannot count", store, unwrapped.Raw()))
		}
	}
	n, err := counter.CountAttestations()
	if err != nil {
		return 0, errors.WithDetail(err, fmt.Sprintf("counted by %T", counter))
	}
	return n, nil
}

// cachedDBStats holds pre-computed database statistics.
type cachedDBStats struct {
	response map[string]any
}

// publishStatsFailure puts the failure in the cache the element reads, so the
// reason reaches whoever is waiting on it.
func (s *QNTXServer) publishStatsFailure(surface string, err error) {
	envelope := newErrorEnvelope(surface, err)
	s.logger.Warnw("Database stats unavailable",
		"surface", surface, "error", err, "error_id", envelope.ID)
	s.dbStatsCache.Store(&cachedDBStats{response: map[string]any{
		"type":  "database_stats",
		"error": envelope,
	}})
}

// startDBStatsRefresher launches a background goroutine that refreshes the
// database stats cache, paced by paceRefreshes.
func (s *QNTXServer) startDBStatsRefresher() {
	s.wg.Go("dbStats.refresher", func() {
		// First refresh runs async — doesn't block startup.
		paceRefreshes(s.ctx, dbStatsRefreshInterval, func() time.Duration {
			started := time.Now()
			landings := s.refreshDBStats()
			took := time.Since(started)
			if took > dbStatsRefreshInterval {
				s.sayOverran(took, landings)
			}
			return took
		})
	})
}

// sayOverran names a refresh that took longer than its interval, and the
// landing whose count took longest within it.
func (s *QNTXServer) sayOverran(took time.Duration, landings []Landing) {
	var slowest Landing
	for _, one := range landings {
		if one.CountTook >= slowest.CountTook {
			slowest = one
		}
	}
	s.logger.Infow("A database stats refresh took longer than its interval",
		"took", took,
		"interval", dbStatsRefreshInterval,
		"next_in", nextRefreshIn(took, dbStatsRefreshInterval),
		"landing", slowest.Namespace,
		"landing_path", slowest.Path,
		"landing_count_took", slowest.CountTook)
}

// refreshDBStats fills the cache, and hands back the landings it read.
func (s *QNTXServer) refreshDBStats() []Landing {
	var totalAttestations int

	statsDB, err := openSQLite(s.dbPath)
	if err != nil {
		s.publishStatsFailure("open stats connection", err)
		return nil
	}
	defer func() { sqlclose.Log(statsDB.Close(), s.logger, "the stats db") }()

	rustdriver.SetCaller("db-stats")
	queryStart := time.Now()

	// The operational tables describe the attestations only when the operational
	// database is the attestation store, which is the sqlite backend.
	dimensionsDescribeTheCount := false
	switch storeCount, countErr := countAttestations(s.held.Served()); {
	case countErr == nil:
		totalAttestations = storeCount
	case errors.Is(countErr, ErrNoAttestationCounter):
		if err := statsDB.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&totalAttestations); err != nil {
			s.publishStatsFailure("count attestations", err)
			return nil
		}
		dimensionsDescribeTheCount = true
	default:
		s.publishStatsFailure("count attestations", countErr)
		return nil
	}
	s.logger.Debugw("DB stats queries complete", "elapsed", time.Since(queryStart), "attestations", totalAttestations)

	// Rich fields
	richStore := storage.NewRichStore(statsDB, nil, s.logger.Named("db-stats-cache"))
	var richFields any
	richFieldsWithStats, err := richStore.GetRichFieldsWithStats(overAtMost)
	if err != nil {
		richFields = richStore.GetDiscoveredRichFields()
	} else {
		richFields = richFieldsWithStats
	}

	// Storage backend info
	storageBackend := "go"
	if syscap.IsStorageOptimized() {
		storageBackend = "rust"
	}

	// Performance snapshot (slow ops + mutex contention)
	perfData := buildPerformanceData()

	// Live system status: write lock, WAL, dilation
	liveStatus := buildLiveStatus(s)

	// What the record has cost, per reader. A backend that keeps everything
	// on the node reports none, because then no read leaves it.
	//
	// Summed across every reporter: the stores that reach the location are
	// opened in more than one place, and a reader missing from this list is a
	// reader nobody can see spending (ADR-037).
	var recordSpend []Spend
	var recordSpendErr error
	if len(s.recordReporters) > 0 {
		recordSpend, recordSpendErr = spentAcross(s.recordReporters)
		s.saySpend(recordSpend)
		if recordSpendErr == nil {
			recordSpendErr = heldOnNode(statsDB, recordSpend)
		}
	}

	// The database behind each namespace. A read is answered from one of these
	// and never from the record (ADR-037), so this is what a reader is looking
	// at when they ask what this node holds.
	var landings []Landing
	var landingsErr error
	if s.landingReporter != nil {
		landings, landingsErr = s.landingReporter.Landings()
	}

	// What the node holds is every database it answers from and not the one it
	// was opened with. The status line reads this same number, so a count of
	// one namespace was the node under-reporting itself everywhere it appears.
	if len(landings) > 0 {
		totalAttestations = 0
		for _, one := range landings {
			totalAttestations += one.Attestations
		}
	}

	response := map[string]any{
		"type":               "database_stats",
		"path":               s.dbPath,
		"storage_backend":    storageBackend,
		"storage_optimized":  syscap.IsStorageOptimized(),
		"storage_version":    syscap.GetStorageVersion(),
		"total_attestations": totalAttestations,
		"rich_fields":        richFields,
		"performance":        perfData,
		"live":               liveStatus,
	}
	switch {
	case landingsErr != nil:
		envelope := newErrorEnvelope("landing files", landingsErr)
		s.logger.Warnw("Landing files unavailable",
			"error", landingsErr, "error_id", envelope.ID)
		response["landings_error"] = envelope
	case landings != nil:
		response["landings"] = landings
	}

	switch {
	case recordSpendErr != nil:
		envelope := newErrorEnvelope("record spend", recordSpendErr)
		s.logger.Warnw("Record spend unavailable",
			"error", recordSpendErr, "error_id", envelope.ID)
		response["record_spend_error"] = envelope
	case recordSpend != nil:
		response["record_spend"] = recordSpend
	}

	// Absent says the operational tables do not describe these attestations.
	// Zero says they do and the answer is none.
	if !dimensionsDescribeTheCount {
		s.dbStatsCache.Store(&cachedDBStats{response: response})
		return landings
	}

	s.dbStatsCache.Store(&cachedDBStats{response: response})
	return landings
}

// buildLiveStatus collects real-time system metrics for the frontend:
// write lock holder, WAL file size, and dilation state.
func buildLiveStatus(s *QNTXServer) map[string]any {
	status := make(map[string]any)

	// Write lock holder
	if s.writeLockInspector != nil {
		holder, held := s.writeLockInspector.WriteHolderInfo()
		if holder != "" {
			status["write_lock"] = map[string]any{
				"holder":  holder,
				"held_ms": held.Milliseconds(),
			}
		}
	}

	// WAL file size (just stat the file — no queries needed)
	walPath := s.dbPath + "-wal"
	if info, err := os.Stat(walPath); err == nil {
		status["wal_bytes"] = info.Size()
	}

	// DB file size
	if info, err := os.Stat(s.dbPath); err == nil {
		status["db_bytes"] = info.Size()
	}

	// Dilation + system pressure
	status["dilation"] = async.CalculateDilation()
	memPct, cpuPct := async.GetPressure()
	status["mem_pct"] = memPct
	status["cpu_pct"] = cpuPct

	return status
}

// parseLegacyPredicates converts old sample_predicates (each entry is a JSON
// array string like "[\"type\"]") into a flat deduplicated list of strings.
func parseLegacyPredicates(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		var parsed []string
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			for _, p := range parsed {
				if !seen[p] {
					seen[p] = true
					result = append(result, p)
				}
			}
		}
	}
	return result
}
