package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
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

// publishStatsFailure puts the failure in the cache the glyph reads, so the
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
	defer statsDB.Close()

	rustdriver.SetCaller("db-stats")
	queryStart := time.Now()

	// The operational tables describe the attestations only when the operational
	// database is the attestation store, which is the sqlite backend.
	dimensionsDescribeTheCount := false
	switch storeCount, countErr := countAttestations(s.atsStore); {
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

	// Recent evictions
	recentEvictions := queryRecentEvictions(statsDB)

	// Performance snapshot (slow ops + mutex contention)
	perfData := buildPerformanceData()

	// Live system status: write lock, WAL, dilation
	liveStatus := buildLiveStatus(s)

	response := map[string]interface{}{
		"type":                 "database_stats",
		"path":                 s.dbPath,
		"storage_backend":      storageBackend,
		"storage_optimized":    syscap.IsStorageOptimized(),
		"storage_version":      syscap.GetStorageVersion(),
		"total_attestations": totalAttestations,
		"rich_fields":        richFields,
		"recent_evictions":   recentEvictions,
		"performance":        perfData,
		"live":               liveStatus,
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
	response["distillation"] = distillStats
	response["predicate_histograms"] = queryPredicateHistograms(statsDB)

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
func queryDistillStats(db *sql.DB) (map[string]interface{}, error) {
	var distillCount int
	var totalPreserved sql.NullInt64
	var oldestDistill, newestDistill sql.NullString

	if err := db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'distill'").
		Scan(&distillCount); err != nil {
		return nil, errors.Wrap(err, "failed to count distilled attestations")
	}
	if distillCount == 0 {
		return nil, nil
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
	defer rows.Close()
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
	defer sigmaRows.Close()

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
func queryPredicateHistograms(db *sql.DB) map[string]map[string]int64 {
	rows, err := db.Query(`
		SELECT jp.predicate, a.attributes
		FROM attestation_predicates jp
		JOIN attestations a ON a.id = jp.attestation_id
		WHERE a.source = 'distill'
		  AND json_extract(a.attributes, '$._histogram') IS NOT NULL
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]map[string]int64)
	for rows.Next() {
		var predicate, attrsJSON string
		if rows.Scan(&predicate, &attrsJSON) != nil {
			continue
		}

		// Strip distill: prefix layers for clean predicate names
		clean := predicate
		for strings.HasPrefix(clean, "distill:") {
			clean = clean[len("distill:"):]
		}

		var attrs map[string]interface{}
		if json.Unmarshal([]byte(attrsJSON), &attrs) != nil {
			continue
		}
		histRaw, ok := attrs["_histogram"]
		if !ok {
			continue
		}
		hist, ok := histRaw.(map[string]interface{})
		if !ok {
			continue
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

	if len(result) == 0 {
		return nil
	}
	return result
}

func queryRecentEvictions(db *sql.DB) []map[string]any {
	var evictions []map[string]any
	rows, err := db.Query(`
		SELECT event_type, actor, context, entity, deletions_count, limit_value, timestamp, eviction_details
		FROM storage_events
		WHERE event_type != 'storage_warning'
		ORDER BY id DESC
		LIMIT 1000
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

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
			continue
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
	return evictions
}
