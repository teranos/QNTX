package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/syscap"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

const distillHandlerName = "distill"

// distillHandler folds old attestations into compressed summaries.
// Implements async.JobHandler for the Pulse scheduler.
type distillHandler struct {
	db        *sql.DB
	atsStore  ats.AttestationStore
	maxAge    time.Duration
	batchSize int
	dryRun    bool
	logger    *zap.SugaredLogger
}

func (h *distillHandler) Name() string { return distillHandlerName }

func (h *distillHandler) Execute(ctx context.Context, job *async.Job) error {
	// Clean up ghost rows (NULL/empty IDs, zero timestamps) on each cycle
	if result, err := h.db.ExecContext(ctx, `DELETE FROM attestations WHERE id IS NULL OR id = '' OR timestamp = '0' OR timestamp < '0002'`); err == nil {
		if n, _ := result.RowsAffected(); n > 0 {
			h.logger.Infow("Cleaned ghost rows", "deleted", n)
		}
	}

	cutoff := time.Now().Add(-h.maxAge).UTC().Format(time.RFC3339)
	h.logger.Infow("Distill query starting", "cutoff", cutoff, "batch_size", h.batchSize)

	// Find old attestations, grouped by (actor, context)
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, subjects, predicates, actors, contexts,
		       timestamp, source, attributes
		FROM attestations
		WHERE timestamp < ?
		ORDER BY timestamp ASC
		LIMIT ?
	`, cutoff, h.batchSize)
	if err != nil {
		return errors.Wrapf(err, "distill query failed (cutoff=%s, batch=%d)", cutoff, h.batchSize)
	}
	defer rows.Close()

	// Load all candidates
	var candidates []*types.As
	scanned := 0
	scanErrors := 0
	for rows.Next() {
		scanned++
		var (
			id                                                     sql.NullString
			subjectsJSON, predicatesJSON, actorsJSON, contextsJSON string
			timestamp, source                                      string
			attrsJSON                                              sql.NullString
		)
		if err := rows.Scan(&id, &subjectsJSON, &predicatesJSON, &actorsJSON, &contextsJSON,
			&timestamp, &source, &attrsJSON); err != nil {
			scanErrors++
			h.logger.Warnw("Failed to scan attestation row", "error", err, "row", scanned)
			continue
		}

		if !id.Valid || id.String == "" {
			continue // skip ghost rows with NULL/empty IDs
		}
		as := &types.As{ID: id.String, Source: source}
		json.Unmarshal([]byte(subjectsJSON), &as.Subjects)
		json.Unmarshal([]byte(predicatesJSON), &as.Predicates)
		json.Unmarshal([]byte(actorsJSON), &as.Actors)
		json.Unmarshal([]byte(contextsJSON), &as.Contexts)
		if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			as.Timestamp = t
		}
		if attrsJSON.Valid {
			json.Unmarshal([]byte(attrsJSON.String), &as.Attributes)
		}

		candidates = append(candidates, as)
	}

	h.logger.Infow("Distill scan complete", "scanned", scanned, "scan_errors", scanErrors, "candidates", len(candidates))

	if len(candidates) == 0 {
		h.logger.Debugw("Nothing to distill", "cutoff", cutoff)
		return nil
	}

	// Group by predicate — one distill attestation per predicate.
	// Subjects collapse into a count, actors/contexts into sets.
	groups := make(map[string][]*types.As)
	for _, as := range candidates {
		predicate := "_"
		if len(as.Predicates) > 0 {
			predicate = as.Predicates[0]
		}
		groups[predicate] = append(groups[predicate], as)
	}

	totalDistilled := 0
	totalCreated := 0

	for predicate, batch := range groups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		distillAs := buildDistillAttestation(batch, predicate)

		if h.dryRun {
			h.logger.Infow("Dry run: would distill",
				"predicate", predicate,
				"count", len(batch),
				"oldest", batch[0].Timestamp.Format(time.RFC3339),
				"newest", batch[len(batch)-1].Timestamp.Format(time.RFC3339))
			continue
		}

		// Insert the distill attestation via normal store path (updates counters)
		if err := h.atsStore.CreateAttestation(distillAs); err != nil {
			h.logger.Warnw("Failed to create distill attestation",
				"predicate", predicate,
				"count", len(batch),
				"error", err)
			continue
		}
		totalCreated++

		// Delete originals — CASCADE handles junction tables,
		// then decrement enforcement counters.
		ids := make([]string, len(batch))
		for i, a := range batch {
			ids[i] = a.ID
		}
		deleted, err := h.deleteAndUpdateCounters(ctx, ids, batch)
		if err != nil {
			h.logger.Warnw("Failed to delete distilled attestations",
				"predicate", predicate,
				"error", err)
			continue
		}
		totalDistilled += deleted
	}

	if h.dryRun {
		h.logger.Infow("Distill dry run complete",
			"groups", len(groups),
			"candidates", len(candidates))
	} else {
		h.logger.Infow("Distill complete",
			"distilled", totalDistilled,
			"created", totalCreated,
			"groups", len(groups))
	}
	return nil
}

// deleteAndUpdateCounters deletes attestations by ID and decrements enforcement counters.
// No explicit transaction — the Pulse worker already holds one via the rustsqlite driver.
func (h *distillHandler) deleteAndUpdateCounters(ctx context.Context, ids []string, batch []*types.As) (int, error) {
	deleted := 0
	for _, id := range ids {
		result, err := h.db.ExecContext(ctx, "DELETE FROM attestations WHERE id = ?", id)
		if err != nil {
			return deleted, errors.Wrapf(err, "delete attestation %s", id)
		}
		n, _ := result.RowsAffected()
		deleted += int(n)
	}

	// Decrement enforcement counters for each (actor, context) pair
	counterDeltas := make(map[string]map[string]int) // actor -> context -> count
	for _, as := range batch {
		for _, actor := range as.Actors {
			if counterDeltas[actor] == nil {
				counterDeltas[actor] = make(map[string]int)
			}
			for _, c := range as.Contexts {
				counterDeltas[actor][c]++
			}
		}
	}

	for actor, contexts := range counterDeltas {
		for c, count := range contexts {
			h.db.ExecContext(ctx,
				"UPDATE enforcement_actor_context SET count = count - ? WHERE actor = ? AND context = ?",
				count, actor, c)
			h.db.ExecContext(ctx,
				"DELETE FROM enforcement_actor_context WHERE actor = ? AND context = ? AND count <= 0",
				actor, c)
		}

		var remaining int
		if err := h.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM enforcement_actor_context WHERE actor = ?", actor,
		).Scan(&remaining); err == nil {
			if remaining == 0 {
				h.db.ExecContext(ctx, "DELETE FROM enforcement_actor_contexts WHERE actor = ?", actor)
			} else {
				h.db.ExecContext(ctx,
					"UPDATE enforcement_actor_contexts SET count = ? WHERE actor = ?",
					remaining, actor)
			}
		}
	}

	return deleted, nil
}

// buildDistillAttestation creates a compressed summary from a batch of attestations
// grouped by predicate. Subjects collapse into a count, actors/contexts into sets.
func buildDistillAttestation(batch []*types.As, predicate string) *types.As {
	now := time.Now()

	subjectSet := make(map[string]bool)
	actorSet := make(map[string]bool)
	contextSet := make(map[string]bool)
	var firstSeen, lastSeen time.Time

	for _, as := range batch {
		for _, s := range as.Subjects {
			subjectSet[s] = true
		}
		for _, a := range as.Actors {
			actorSet[a] = true
		}
		for _, c := range as.Contexts {
			contextSet[c] = true
		}
		if firstSeen.IsZero() || as.Timestamp.Before(firstSeen) {
			firstSeen = as.Timestamp
		}
		if as.Timestamp.After(lastSeen) {
			lastSeen = as.Timestamp
		}
	}

	// Actors/contexts as sets (capped at 50 to avoid huge distill attestations)
	actors := setToSlice(actorSet, 50)
	contexts := setToSlice(contextSet, 50)

	// Merge attributes
	merged := mergeAttributes(batch)
	merged["_distill"] = true
	merged["_version"] = version.VersionTag + " (" + version.CommitHash + ")"
	merged["_rust_version"] = syscap.GetStorageVersion()
	merged["_count"] = len(batch)

	// _total: transitive total of original observations this attestation represents.
	// For raw attestations, each counts as 1. For prior distill attestations,
	// use their _total (or fall back to _count for pre-_total distill attestations).
	total := 0
	for _, as_ := range batch {
		if as_.Attributes != nil {
			if t, ok := as_.Attributes["_total"].(float64); ok {
				total += int(t)
			} else if c, ok := as_.Attributes["_count"].(float64); ok {
				total += int(c)
			} else {
				total += 1
			}
		} else {
			total += 1
		}
	}
	merged["_total"] = total

	merged["_subjects_count"] = len(subjectSet)
	merged["_first_seen"] = firstSeen.UTC().Format(time.RFC3339)
	merged["_last_seen"] = lastSeen.UTC().Format(time.RFC3339)

	// Keep a sample of subjects (up to 10) for debuggability
	if len(subjectSet) <= 10 {
		subjects := make([]string, 0, len(subjectSet))
		for s := range subjectSet {
			subjects = append(subjects, s)
		}
		merged["_subjects_sample"] = subjects
	} else {
		sample := make([]string, 0, 10)
		for s := range subjectSet {
			sample = append(sample, s)
			if len(sample) >= 10 {
				break
			}
		}
		merged["_subjects_sample"] = sample
	}

	// Strip existing distill: prefixes before adding one. Without this,
	// meta-distillation stacks prefixes infinitely: distill:distill:distill:...
	// Each cycle adds one prefix, so after N cycles a predicate becomes
	// "distill:" repeated N times. The strip loop collapses it to one.
	basePredicate := predicate
	for strings.HasPrefix(basePredicate, "distill:") {
		basePredicate = basePredicate[len("distill:"):]
	}
	distillPredicate := "distill:" + basePredicate

	return &types.As{
		ID:         fmt.Sprintf("AS-distill-%d", now.UnixNano()),
		Subjects:   []string{distillPredicate},
		Predicates: []string{distillPredicate},
		Contexts:   contexts,
		Actors:     actors,
		Timestamp:  now,
		CreatedAt:  now,
		Source:     "distill",
		Attributes: merged,
	}
}

func setToSlice(set map[string]bool, cap int) []string {
	result := make([]string, 0, len(set))
	for s := range set {
		result = append(result, s)
		if len(result) >= cap {
			break
		}
	}
	return result
}

type distillNumAgg struct {
	min, max, sum float64
	count         int
}

type distillStrAgg struct {
	values map[string]bool
	count  int
}

// mergeAttributes mechanically merges attributes from a batch of attestations.
//   - Numbers: {min, max, sum, count}
//   - Strings: {values: [...], count} (values capped at 50)
//   - Constants (same value across all): kept as scalar
//   - Already-aggregated (_distill): merge aggregates
func mergeAttributes(batch []*types.As) map[string]interface{} {
	if len(batch) == 0 {
		return make(map[string]interface{})
	}

	nums := make(map[string]*distillNumAgg)
	strs := make(map[string]*distillStrAgg)
	constants := make(map[string]interface{})
	seen := make(map[string]int) // how many attestations have this key

	for _, as := range batch {
		if as.Attributes == nil {
			continue
		}
		for k, v := range as.Attributes {
			// Skip distill metadata keys
			if k == "_distill" || k == "_count" || k == "_total" || k == "_first_seen" || k == "_last_seen" || k == "_version" || k == "_rust_version" || k == "_subjects_count" || k == "_subjects_sample" {
				continue
			}

			seen[k]++

			switch val := v.(type) {
			case float64:
				if agg, ok := nums[k]; ok {
					if val < agg.min {
						agg.min = val
					}
					if val > agg.max {
						agg.max = val
					}
					agg.sum += val
					agg.count++
				} else {
					nums[k] = &distillNumAgg{min: val, max: val, sum: val, count: 1}
				}
			case string:
				if agg, ok := strs[k]; ok {
					if len(agg.values) < 50 {
						agg.values[val] = true
					}
					agg.count++
				} else {
					strs[k] = &distillStrAgg{values: map[string]bool{val: true}, count: 1}
				}
			case map[string]interface{}:
				// Already-aggregated from prior distill — merge aggregates
				if hasAggKeys(val) {
					if agg, ok := nums[k]; ok {
						mergeNumAgg(agg, val)
					} else {
						nums[k] = numAggFromMap(val)
					}
				}
			default:
				// Track for constant detection
				if prev, ok := constants[k]; ok {
					if fmt.Sprintf("%v", prev) != fmt.Sprintf("%v", v) {
						constants[k] = nil // not constant
					}
				} else {
					constants[k] = v
				}
			}
		}
	}

	result := make(map[string]interface{})

	for k, agg := range nums {
		result[k] = map[string]interface{}{
			"min":   agg.min,
			"max":   agg.max,
			"sum":   agg.sum,
			"count": agg.count,
		}
	}

	for k, agg := range strs {
		if agg.count == len(batch) && len(agg.values) == 1 {
			// Constant string — keep as scalar
			for v := range agg.values {
				result[k] = v
			}
		} else {
			values := make([]string, 0, len(agg.values))
			for v := range agg.values {
				values = append(values, v)
			}
			result[k] = map[string]interface{}{
				"values": values,
				"count":  agg.count,
			}
		}
	}

	// Constants that weren't captured by nums or strs
	for k, v := range constants {
		if _, ok := result[k]; ok {
			continue
		}
		if v != nil {
			result[k] = v
		}
	}

	return result
}

func hasAggKeys(m map[string]interface{}) bool {
	_, hasMin := m["min"]
	_, hasMax := m["max"]
	_, hasSum := m["sum"]
	return hasMin && hasMax && hasSum
}

func numAggFromMap(m map[string]interface{}) *distillNumAgg {
	agg := &distillNumAgg{}
	if v, ok := m["min"].(float64); ok {
		agg.min = v
	}
	if v, ok := m["max"].(float64); ok {
		agg.max = v
	}
	if v, ok := m["sum"].(float64); ok {
		agg.sum = v
	}
	if v, ok := m["count"].(float64); ok {
		agg.count = int(v)
	}
	return agg
}

func mergeNumAgg(agg *distillNumAgg, m map[string]interface{}) {
	if v, ok := m["min"].(float64); ok && v < agg.min {
		agg.min = v
	}
	if v, ok := m["max"].(float64); ok && v > agg.max {
		agg.max = v
	}
	if v, ok := m["sum"].(float64); ok {
		agg.sum += v
	}
	if v, ok := m["count"].(float64); ok {
		agg.count += int(v)
	}
}

// setupDistillSchedule registers the distill handler and auto-creates
// a Pulse schedule if distill.interval_seconds is set.
func (s *QNTXServer) setupDistillSchedule(cfg *appcfg.Config) {
	if cfg.Distill.IntervalSeconds == nil {
		return
	}
	interval := *cfg.Distill.IntervalSeconds
	if interval <= 0 {
		return
	}

	maxAgeHours := cfg.Distill.MaxAgeHours
	if maxAgeHours <= 0 {
		maxAgeHours = 720
	}
	batchSize := cfg.Distill.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	handler := &distillHandler{
		db:        s.db,
		atsStore:  s.atsStore,
		maxAge:    time.Duration(maxAgeHours) * time.Hour,
		batchSize: batchSize,
		dryRun:    cfg.Distill.DryRun,
		logger:    s.logger.Named("distill"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered distill handler",
		"max_age_hours", maxAgeHours,
		"batch_size", batchSize,
		"dry_run", cfg.Distill.DryRun)

	schedStore := schedule.NewStore(s.db)

	// Check for existing schedule to avoid duplicates on restart
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for distill idempotency check",
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == distillHandlerName && j.State == schedule.StateActive {
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update distill schedule interval",
						"job_id", j.ID, "error", err)
				} else {
					s.logger.Infow("Updated distill schedule interval",
						"job_id", j.ID, "interval_seconds", interval)
				}
			}
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_distill_%d", now.Unix()),
		HandlerName:     distillHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
		NextRunAt:       &now, // Run immediately on first startup
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create distill schedule",
			"interval_seconds", interval, "error", err)
		return
	}
	s.logger.Infow("Auto-created distill schedule",
		"job_id", job.ID,
		"interval_seconds", interval,
		"max_age_hours", maxAgeHours,
		"batch_size", batchSize)
}
