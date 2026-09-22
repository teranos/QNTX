package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"os"
	"sort"
	"time"

	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/syscap"
	"github.com/teranos/errors"
)

// WriteLockInspector exposes write lock holder diagnostics.
// Implemented by RustStore.WriteHolderInfo.
type WriteLockInspector interface {
	WriteHolderInfo() (holder string, held time.Duration)
}

const dbStatsRefreshInterval = 30 * time.Second

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
// write-ahead log have grown, and how many attestations it answers from.
//
// The dimensions are filled by the server rather than the backend, because
// they are read with the same driver the stats connection already uses.
type Landing struct {
	Namespace    string `json:"namespace"`
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	WalBytes     int64  `json:"wal_bytes"`
	Attestations int    `json:"attestations"`
	Actors       int    `json:"actors"`
	Subjects     int    `json:"subjects"`
	Contexts     int    `json:"contexts"`

	TopPredicates []Common `json:"top_predicates"`
	TopContexts   []Common `json:"top_contexts"`

	// Over is when this namespace's attestations landed, by the hour, which is
	// the line the chart draws for it.
	Over map[string]int64 `json:"over"`
}

// A Common is one value a namespace uses often, and how often. What a panel
// shows to say what a namespace is about without reading any of it.
type Common struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// commonTo is the values one column uses most, most first.
func commonTo(db *sql.DB, query string, most int) (_ []Common, err error) {
	rows, err := db.Query(query, most)
	if err != nil {
		return nil, err
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for commonTo") }()

	var common []Common
	for rows.Next() {
		var one Common
		if err := rows.Scan(&one.Name, &one.Count); err != nil {
			return nil, err
		}
		common = append(common, one)
	}
	return common, rows.Err()
}

// dimensionsOf counts the distinct actors, subjects and contexts one landing
// file holds. These are its own tables (ADR-037): the file answers every read
// of that namespace, so it is the file that describes it.
func (s *QNTXServer) dimensionsOf(landing *Landing) error {
	db, err := sql.Open("rustsqlite", landing.Path)
	if err != nil {
		return errors.Wrapf(err, "the landing file of %s at %s did not open", landing.Namespace, landing.Path)
	}
	defer func() { sqlclose.Log(db.Close(), s.logger, "the landing file of "+landing.Namespace) }()

	for _, counting := range []struct {
		query string
		into  *int
	}{
		{"SELECT COUNT(DISTINCT actor) FROM attestation_actors", &landing.Actors},
		{"SELECT COUNT(DISTINCT subject) FROM attestation_subjects", &landing.Subjects},
		{"SELECT COUNT(DISTINCT context) FROM attestation_contexts", &landing.Contexts},
	} {
		if err := db.QueryRow(counting.query).Scan(counting.into); err != nil {
			return errors.Wrapf(err, "%s did not answer for %s", counting.query, landing.Namespace)
		}
	}

	landing.TopPredicates, err = commonTo(db,
		"SELECT predicate, COUNT(*) AS held FROM attestation_predicates "+
			"GROUP BY predicate ORDER BY held DESC LIMIT ?", commonAtMost)
	if err != nil {
		return errors.Wrapf(err, "the predicates of %s did not answer", landing.Namespace)
	}

	landing.TopContexts, err = commonTo(db,
		"SELECT context, COUNT(*) AS held FROM attestation_contexts "+
			"GROUP BY context ORDER BY held DESC LIMIT ?", commonAtMost)
	if err != nil {
		return errors.Wrapf(err, "the contexts of %s did not answer", landing.Namespace)
	}

	landing.Over, err = overTime(db)
	if err != nil {
		return errors.Wrapf(err, "when the attestations of %s landed did not answer", landing.Namespace)
	}
	return nil
}

// commonAtMost is how many of each a row shows. Enough to say what a namespace
// is about; more is a list nobody reads.
const commonAtMost = 8

// overAtMost is how many buckets one namespace's line is drawn from. Hours,
// so a fortnight fits and the chart still has a shape.
const overAtMost = 336

// overTime is when a namespace's attestations landed, by the hour. The old
// chart read distillation output, which a node that persists cheaply to the
// record does not produce; this reads the attestations themselves.
func overTime(db *sql.DB) (_ map[string]int64, err error) {
	rows, err := db.Query(`
		SELECT strftime('%Y-%m-%dT%H', timestamp) AS bucket, COUNT(*) AS held
		FROM attestations
		WHERE bucket IS NOT NULL
		GROUP BY bucket
		ORDER BY bucket DESC
		LIMIT ?`, overAtMost)
	if err != nil {
		return nil, err
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for overTime") }()

	over := map[string]int64{}
	for rows.Next() {
		var bucket string
		var held int64
		if err := rows.Scan(&bucket, &held); err != nil {
			return nil, err
		}
		over[bucket] = held
	}
	return over, rows.Err()
}

// Spend is one reader and what it has cost against the record: the name make
// parity gives it, the request, and how many. A row here that parity calls
// record-only is a read that never has to leave the node.
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
			summed[Spend{Of: one.Of, Request: one.Request, HeldOnNode: one.HeldOnNode}] += one.Count
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
		seen := Spend{Of: one.Of, Request: one.Request, HeldOnNode: one.HeldOnNode}
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
	response map[string]interface{}
}

// publishStatsFailure puts the failure in the cache the element reads, so the
// reason reaches whoever is waiting on it.
func (s *QNTXServer) publishStatsFailure(surface string, err error) {
	envelope := newErrorEnvelope(surface, err)
	s.logger.Warnw("Database stats unavailable",
		"surface", surface, "error", err, "error_id", envelope.ID)
	s.dbStatsCache.Store(&cachedDBStats{response: map[string]interface{}{
		"type":  "database_stats",
		"error": envelope,
	}})
}

// startDBStatsRefresher launches a background goroutine that refreshes
// the database stats cache every 30 seconds.
func (s *QNTXServer) startDBStatsRefresher() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// First refresh runs async — doesn't block startup.
		s.refreshDBStats()

		ticker := time.NewTicker(dbStatsRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.refreshDBStats()
			}
		}
	}()
}

func (s *QNTXServer) refreshDBStats() {
	var totalAttestations, uniqueActors, uniqueSubjects, uniqueContexts int

	// Use the rustsqlite driver — same SQLite library instance as the write path.
	// Opening a separate Go sqlite3 connection causes WAL checkpoint corruption
	// because mattn/go-sqlite3 and rusqlite are independent SQLite C libraries
	// with separate WAL-index mappings.
	statsDB, err := sql.Open("rustsqlite", s.dbPath)
	if err != nil {
		s.publishStatsFailure("open stats connection", err)
		return
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
			return
		}
		dimensionsDescribeTheCount = true
	default:
		s.publishStatsFailure("count attestations", countErr)
		return
	}

	// A count that failed and a count of zero are the same number on the way
	// out, so the cache would publish "no actors" for a query that never ran.
	for _, count := range []struct {
		query string
		into  *int
	}{
		{"SELECT COUNT(DISTINCT actor) FROM attestation_actors", &uniqueActors},
		{"SELECT COUNT(DISTINCT subject) FROM attestation_subjects", &uniqueSubjects},
		{"SELECT COUNT(DISTINCT context) FROM attestation_contexts", &uniqueContexts},
	} {
		if !dimensionsDescribeTheCount {
			break
		}
		if err := statsDB.QueryRow(count.query).Scan(count.into); err != nil {
			s.publishStatsFailure("count "+count.query, err)
			return
		}
	}
	s.logger.Debugw("DB stats queries complete", "elapsed", time.Since(queryStart), "attestations", totalAttestations)

	// Rich fields
	boundedStore := storage.NewBoundedStore(statsDB, nil, s.logger.Named("db-stats-cache"))
	var richFields interface{}
	richFieldsWithStats, err := boundedStore.GetRichFieldsWithStats()
	if err != nil {
		richFields = boundedStore.GetDiscoveredRichFields()
	} else {
		richFields = richFieldsWithStats
	}

	// Storage backend info
	storageBackend := "go"
	if syscap.IsStorageOptimized() {
		storageBackend = "rust"
	}

	// Recent evictions. Beside the rest rather than instead of it: what was
	// deleted to stay inside a limit is worth knowing, and not knowing it is
	// not a reason to blank the panel.
	recentEvictions, evictionsErr := queryRecentEvictions(statsDB)

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
	}

	// The database behind each namespace. A read is answered from one of these
	// and never from the record (ADR-037), so this is what a reader is looking
	// at when they ask what this node holds.
	var landings []Landing
	var landingsErr error
	if s.landingReporter != nil {
		landings, landingsErr = s.landingReporter.Landings()
		for at := range landings {
			// One that will not say its dimensions still says its size and its
			// count, so the row is drawn with what it did answer.
			if err := s.dimensionsOf(&landings[at]); err != nil {
				s.logger.Warnw("A landing file did not say what it holds",
					"namespace", landings[at].Namespace, "error", err)
			}
		}
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

	response := map[string]interface{}{
		"type":               "database_stats",
		"path":               s.dbPath,
		"storage_backend":    storageBackend,
		"storage_optimized":  syscap.IsStorageOptimized(),
		"storage_version":    syscap.GetStorageVersion(),
		"total_attestations": totalAttestations,
		"rich_fields":        richFields,
		"recent_evictions":   recentEvictions,
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

	if evictionsErr != nil {
		envelope := newErrorEnvelope("recent evictions", evictionsErr)
		s.logger.Warnw("Recent evictions unavailable",
			"error", evictionsErr, "error_id", envelope.ID)
		response["recent_evictions_error"] = envelope
	}

	// Absent says the operational tables do not describe these attestations.
	// Zero says they do and the answer is none.
	if !dimensionsDescribeTheCount {
		s.dbStatsCache.Store(&cachedDBStats{response: response})
		return
	}

	response["unique_actors"] = uniqueActors
	response["unique_subjects"] = uniqueSubjects
	response["unique_contexts"] = uniqueContexts

	// Distillation folds attestations held in this database. A backend keeping
	// them elsewhere has no distillation to report and no key for it.
	distillStats, err := queryDistillStats(statsDB)
	if err != nil {
		s.publishStatsFailure("distillation stats", err)
		return
	}
	if len(distillStats) > 0 {
		response["distillation"] = distillStats
	}

	// One surface that could not be read does not take the rest of the panel
	// with it. The failure travels beside the fields that did answer, so a
	// reader sees which is missing rather than an empty cache.
	histograms, err := queryPredicateHistograms(statsDB)
	if err != nil {
		envelope := newErrorEnvelope("predicate histograms", err)
		s.logger.Warnw("Predicate histograms unavailable", "error", err, "error_id", envelope.ID)
		response["predicate_histograms_error"] = envelope
	} else {
		response["predicate_histograms"] = histograms
	}

	s.dbStatsCache.Store(&cachedDBStats{response: response})
}

// buildLiveStatus collects real-time system metrics for the frontend:
// write lock holder, WAL file size, and dilation state.
func buildLiveStatus(s *QNTXServer) map[string]interface{} {
	status := make(map[string]interface{})

	// Write lock holder
	if s.writeLockInspector != nil {
		holder, held := s.writeLockInspector.WriteHolderInfo()
		if holder != "" {
			status["write_lock"] = map[string]interface{}{
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

// buildPerformanceData converts the slow log collector's rolling history
// into a JSON-friendly structure for the frontend.
func buildPerformanceData() map[string]interface{} {
	snap := sqlitecgo.GetPerformanceSnapshot()
	if snap.Current == nil {
		return nil
	}

	// Current window: operations sorted by variance (max-min spread)
	type opEntry struct {
		name     string
		stats    *sqlitecgo.BucketStats
		variance float64
	}
	var ops []opEntry
	for name, stats := range snap.Current {
		spread := stats.Max - stats.Min
		variance := float64(spread) / float64(stats.Avg+1) // relative variance
		ops = append(ops, opEntry{name, stats, variance})
	}
	// Sort by variance descending
	for i := 0; i < len(ops); i++ {
		for j := i + 1; j < len(ops); j++ {
			if ops[j].variance > ops[i].variance {
				ops[i], ops[j] = ops[j], ops[i]
			}
		}
	}

	var current []map[string]interface{}
	for _, op := range ops {
		kind := "op"
		name := op.name
		if strings.HasPrefix(name, "mutex:") {
			kind = "mutex"
			name = strings.TrimPrefix(name, "mutex:")
		}
		current = append(current, map[string]interface{}{
			"name":  name,
			"kind":  kind,
			"count": op.stats.Count,
			"min":   op.stats.Min.Milliseconds(),
			"max":   op.stats.Max.Milliseconds(),
			"avg":   op.stats.Avg.Milliseconds(),
		})
	}

	// History: per-operation avg over time (for sparklines)
	// Collect all operation names seen across history
	allOps := make(map[string]bool)
	for _, window := range snap.History {
		for name := range window {
			allOps[name] = true
		}
	}

	sparklines := make(map[string][]interface{})
	for name := range allOps {
		series := make([]interface{}, len(snap.History))
		for i, window := range snap.History {
			if stats, ok := window[name]; ok {
				series[i] = stats.Avg.Milliseconds()
			} else {
				series[i] = nil
			}
		}
		sparklines[name] = series
	}

	return map[string]interface{}{
		"current":    current,
		"sparklines": sparklines,
		"windows":    len(snap.History),
	}
}

// parseLegacyPredicates converts old sample_predicates (each entry is a JSON
// array string like "[\"type\"]") into a flat deduplicated list of strings.
func parseLegacyPredicates(raw interface{}) []string {
	arr, ok := raw.([]interface{})
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

// queryDistillStats returns nil when nothing has been distilled, and an error
// when it could not find out — which are different answers.
func queryDistillStats(db *sql.DB) (_ map[string]interface{}, err error) {
	var distillCount int
	var totalPreserved sql.NullInt64
	var oldestDistill, newestDistill sql.NullString

	if err := db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'distill'").
		Scan(&distillCount); err != nil {
		return nil, errors.Wrap(err, "failed to count distilled attestations")
	}
	if distillCount == 0 {
		return map[string]interface{}{}, nil
	}

	if err := db.QueryRow(`
		SELECT SUM(json_extract(attributes, '$._count')),
		       MIN(json_extract(attributes, '$._first_seen')),
		       MAX(json_extract(attributes, '$._last_seen'))
		FROM attestations WHERE source = 'distill'
		  AND json_extract(attributes, '$._first_seen') > '0002'
	`).Scan(&totalPreserved, &oldestDistill, &newestDistill); err != nil {
		return nil, errors.Wrap(err, "failed to summarize distilled attestations")
	}

	result := map[string]interface{}{
		"sigmas": distillCount,
	}
	if totalPreserved.Valid {
		result["preserved_count"] = totalPreserved.Int64
	}
	if oldestDistill.Valid {
		result["oldest"] = oldestDistill.String
	}
	if newestDistill.Valid {
		result["newest"] = newestDistill.String
	}

	// Top distill predicates
	rows, err := db.Query(`
		SELECT jp.predicate, COUNT(*) as cnt
		FROM attestation_predicates jp
		JOIN attestations a ON a.id = jp.attestation_id
		WHERE a.source = 'distill'
		GROUP BY jp.predicate
		ORDER BY cnt DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query distill predicates")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for queryDistillStats") }()
	var predicates []map[string]interface{}
	for rows.Next() {
		var pred string
		var cnt int
		if err := rows.Scan(&pred, &cnt); err != nil {
			return nil, errors.Wrap(err, "failed to scan a distill predicate")
		}
		predicates = append(predicates, map[string]interface{}{
			"predicate": pred,
			"count":     cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate distill predicates")
	}
	if len(predicates) > 0 {
		result["predicates"] = predicates
	}

	// Top sigmas ranked by total observations (>= 100 obs only)
	// Includes full row data so the frontend can open sigma windows on click.
	sigmaRows, err := db.Query(`
		SELECT id, subjects, predicates, actors, contexts,
		       timestamp, source, attributes
		FROM attestations
		WHERE source = 'distill'
		  AND COALESCE(json_extract(attributes, '$._total'), json_extract(attributes, '$._count'), 0) >= 100
		ORDER BY COALESCE(json_extract(attributes, '$._total'), json_extract(attributes, '$._count'), 0) DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query top sigmas")
	}
	defer func() { err = sqlclose.With(err, sigmaRows.Close(), "the sigma rows") }()

	var topSigmas []map[string]interface{}
	for sigmaRows.Next() {
		var id, subjects, predicates, actors, contexts, source string
		var timestamp sql.NullString
		var attributes string
		if err := sigmaRows.Scan(&id, &subjects, &predicates, &actors, &contexts,
			&timestamp, &source, &attributes); err != nil {
			return nil, errors.Wrap(err, "failed to scan a top sigma")
		}
		sigma := map[string]interface{}{
			"id":         id,
			"subjects":   subjects,
			"predicates": predicates,
			"actors":     actors,
			"contexts":   contexts,
			"source":     source,
			"attributes": attributes,
		}
		if timestamp.Valid {
			sigma["timestamp"] = timestamp.String
		}
		topSigmas = append(topSigmas, sigma)
	}
	if err := sigmaRows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate top sigmas")
	}
	if len(topSigmas) > 0 {
		result["top_sigmas"] = topSigmas
	}

	return result, nil
}

// queryPredicateHistograms aggregates _histogram data from distill attestations
// grouped by predicate. Returns map[predicate] -> map[timeKey] -> count.
func queryPredicateHistograms(db *sql.DB) (_ map[string]map[string]int64, err error) {
	rows, err := db.Query(`
		SELECT jp.predicate, a.attributes
		FROM attestation_predicates jp
		JOIN attestations a ON a.id = jp.attestation_id
		WHERE a.source = 'distill'
		  AND json_extract(a.attributes, '$._histogram') IS NOT NULL
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the predicate histograms")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for queryPredicateHistograms") }()

	result := make(map[string]map[string]int64)
	for rows.Next() {
		var predicate, attrsJSON string
		if err := rows.Scan(&predicate, &attrsJSON); err != nil {
			return nil, errors.Wrap(err, "failed to scan a predicate histogram")
		}

		// Strip distill: prefix layers for clean predicate names
		clean := predicate
		for strings.HasPrefix(clean, "distill:") {
			clean = clean[len("distill:"):]
		}

		// The row was selected for having a histogram, so one that will not
		// parse is a row nobody can read rather than a row without one.
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
			return nil, errors.Wrapf(err, "the attributes of a %s histogram do not parse", clean)
		}
		histRaw, ok := attrs["_histogram"]
		if !ok {
			continue
		}
		hist, ok := histRaw.(map[string]interface{})
		if !ok {
			return nil, errors.Newf("the _histogram of %s is %T, not an object", clean, histRaw)
		}

		if result[clean] == nil {
			result[clean] = make(map[string]int64)
		}
		for key, val := range hist {
			switch v := val.(type) {
			case float64:
				result[clean][key] += int64(v)
			case json.Number:
				if n, err := v.Int64(); err == nil {
					result[clean][key] += n
				}
			}
		}
	}

	// A read that stopped partway hands back what it managed, which reads as
	// the whole of what is there.
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "the predicate histograms stopped partway")
	}
	// An empty map already says no distill attestation carried a histogram.
	// Nil on top of it is a second way to say the same thing, and the caller
	// then has two absences to tell apart.
	return result, nil
}

// An eviction is data this node deleted to stay inside its limits. A read that
// could not say what was evicted must not answer as a node that evicted
// nothing.
func queryRecentEvictions(db *sql.DB) (_ []map[string]any, err error) {
	var evictions []map[string]any
	rows, err := db.Query(`
		SELECT event_type, actor, context, entity, deletions_count, limit_value, timestamp, eviction_details
		FROM storage_events
		WHERE event_type != 'storage_warning'
		ORDER BY id DESC
		LIMIT 1000
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the recent evictions")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for queryRecentEvictions") }()

	for rows.Next() {
		var (
			eventType       string
			actor           sql.NullString
			ctx             sql.NullString
			entity          sql.NullString
			deletionsCount  int
			limitValue      sql.NullInt64
			timestamp       string
			evictionDetails sql.NullString
		)
		if err := rows.Scan(&eventType, &actor, &ctx, &entity, &deletionsCount, &limitValue, &timestamp, &evictionDetails); err != nil {
			return nil, errors.Wrap(err, "failed to scan a storage eviction")
		}
		limit := int(limitValue.Int64)
		if !limitValue.Valid {
			limit = 0
		}
		var message string
		switch eventType {
		case "actor_context_limit":
			message = fmt.Sprintf("Evicted %d old attestations for %s/%s (limit: %d)", deletionsCount, actor.String, ctx.String, limit)
		case "actor_contexts_limit":
			message = fmt.Sprintf("Evicted %d attestations for actor %s (contexts limit: %d)", deletionsCount, actor.String, limit)
		case "entity_actors_limit":
			message = fmt.Sprintf("Evicted %d attestations for entity %s (actors limit: %d)", deletionsCount, entity.String, limit)
		default:
			message = fmt.Sprintf("Evicted %d attestations (%s)", deletionsCount, eventType)
		}

		ev := map[string]any{
			"event_type":      eventType,
			"actor":           actor.String,
			"context":         ctx.String,
			"entity":          entity.String,
			"deletions_count": deletionsCount,
			"message":         message,
			"timestamp":       timestamp,
		}

		if evictionDetails.Valid && evictionDetails.String != "" {
			var details map[string]any
			if err := json.Unmarshal([]byte(evictionDetails.String), &details); err == nil {
				if preds, ok := details["predicates"]; ok {
					ev["predicates"] = preds
				} else if rawPreds, ok := details["sample_predicates"]; ok {
					// Legacy format: each entry is a JSON array string like "[\"type\"]"
					ev["predicates"] = parseLegacyPredicates(rawPreds)
				}
				if ls, ok := details["last_seen"]; ok {
					ev["last_seen"] = ls
				}
			}
		}

		evictions = append(evictions, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "the recent evictions stopped partway")
	}
	return evictions, nil
}
