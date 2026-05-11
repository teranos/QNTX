package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/server/syscap"
)

const dbStatsRefreshInterval = 30 * time.Second

// cachedDBStats holds pre-computed database statistics.
type cachedDBStats struct {
	response map[string]interface{}
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

	// Open a dedicated read connection — bypasses the shared pool which can be
	// starved by Rust FFI, backups, and plugin queries at startup.
	statsDB, err := sql.Open("sqlite3", s.dbPath+"?mode=ro&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		s.logger.Warnw("Failed to open stats connection", "error", err)
		return
	}
	defer statsDB.Close()
	statsDB.SetMaxOpenConns(1)

	queryStart := time.Now()
	if err := statsDB.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&totalAttestations); err != nil {
		s.logger.Warnw("Failed to refresh database stats cache", "error", err, "elapsed", time.Since(queryStart))
		return
	}
	_ = statsDB.QueryRow("SELECT COUNT(DISTINCT actor) FROM attestation_actors").Scan(&uniqueActors)
	_ = statsDB.QueryRow("SELECT COUNT(DISTINCT subject) FROM attestation_subjects").Scan(&uniqueSubjects)
	_ = statsDB.QueryRow("SELECT COUNT(DISTINCT context) FROM attestation_contexts").Scan(&uniqueContexts)
	s.logger.Infow("DB stats queries complete", "elapsed", time.Since(queryStart), "attestations", totalAttestations)

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

	response := map[string]interface{}{
		"type":               "database_stats",
		"path":               s.dbPath,
		"storage_backend":    storageBackend,
		"storage_optimized":  syscap.IsStorageOptimized(),
		"storage_version":    syscap.GetStorageVersion(),
		"total_attestations": totalAttestations,
		"unique_actors":      uniqueActors,
		"unique_subjects":    uniqueSubjects,
		"unique_contexts":    uniqueContexts,
		"rich_fields":        richFields,
		"recent_evictions":   recentEvictions,
		"performance":        perfData,
	}

	s.dbStatsCache.Store(&cachedDBStats{response: response})
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

func queryRecentEvictions(db *sql.DB) []map[string]interface{} {
	var evictions []map[string]interface{}
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

		ev := map[string]interface{}{
			"event_type":      eventType,
			"actor":           actor.String,
			"context":         ctx.String,
			"entity":          entity.String,
			"deletions_count": deletionsCount,
			"message":         message,
			"timestamp":       timestamp,
		}

		if evictionDetails.Valid && evictionDetails.String != "" {
			var details map[string]interface{}
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
