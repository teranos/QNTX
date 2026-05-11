package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/storage"
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

	// Query stats directly via Go's *sql.DB read pool — avoids Rust FFI write mutex contention.
	if err := s.db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&totalAttestations); err != nil {
		s.logger.Warnw("Failed to refresh database stats cache", "error", err)
		return
	}
	_ = s.db.QueryRow("SELECT COUNT(DISTINCT actor) FROM attestation_actors").Scan(&uniqueActors)
	_ = s.db.QueryRow("SELECT COUNT(DISTINCT subject) FROM attestation_subjects").Scan(&uniqueSubjects)
	_ = s.db.QueryRow("SELECT COUNT(DISTINCT context) FROM attestation_contexts").Scan(&uniqueContexts)

	// Rich fields
	boundedStore := storage.NewBoundedStore(s.db, nil, s.logger.Named("db-stats-cache"))
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
	recentEvictions := s.queryRecentEvictions()

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
	}

	s.dbStatsCache.Store(&cachedDBStats{response: response})
}

func (s *QNTXServer) queryRecentEvictions() []map[string]interface{} {
	var evictions []map[string]interface{}
	rows, err := s.db.Query(`
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
