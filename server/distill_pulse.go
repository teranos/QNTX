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
	h.logger.Debugw("Σ Sigma query starting", "cutoff", cutoff, "batch_size", h.batchSize)

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

	h.logger.Debugw("Σ Sigma scan complete", "scanned", scanned, "scan_errors", scanErrors, "candidates", len(candidates))

	if len(candidates) == 0 {
		h.logger.Debugw("Σ Nothing to distill", "cutoff", cutoff)
		return nil
	}

	// Group by predicate — one sigma per predicate.
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

		buildStart := time.Now()
		distillAs := buildDistillAttestation(batch, predicate)
		buildDur := time.Since(buildStart)

		if h.dryRun {
			h.logger.Infow("Dry run: would distill",
				"predicate", predicate,
				"count", len(batch),
				"oldest", batch[0].Timestamp.Format(time.RFC3339),
				"newest", batch[len(batch)-1].Timestamp.Format(time.RFC3339))
			continue
		}

		// Insert sigma via normal store path (updates counters)
		createStart := time.Now()
		if err := h.atsStore.CreateAttestation(distillAs); err != nil {
			h.logger.Warnw("Σ Failed to create sigma",
				"predicate", predicate,
				"count", len(batch),
				"error", err)
			continue
		}
		createDur := time.Since(createStart)
		totalCreated++

		// Delete originals — CASCADE handles junction tables,
		// then decrement enforcement counters.
		ids := make([]string, len(batch))
		for i, a := range batch {
			ids[i] = a.ID
		}
		deleteStart := time.Now()
		deleted, err := h.deleteAndUpdateCounters(ctx, ids, batch)
		deleteDur := time.Since(deleteStart)
		if err != nil {
			h.logger.Warnw("Failed to delete distilled attestations",
				"predicate", predicate,
				"error", err)
			continue
		}
		totalDistilled += deleted

		h.logger.Debugw("Σ Sigma group timing",
			"predicate", predicate,
			"batch_size", len(batch),
			"build_ms", buildDur.Milliseconds(),
			"create_ms", createDur.Milliseconds(),
			"delete_ms", deleteDur.Milliseconds())
	}

	if h.dryRun {
		h.logger.Infow("Σ Sigma dry run complete",
			"groups", len(groups),
			"candidates", len(candidates))
	} else {
		h.logger.Infow("Σ Sigma complete",
			"distilled", totalDistilled,
			"sigmas_created", totalCreated,
			"groups", len(groups))
	}
	return nil
}

// deleteAndUpdateCounters deletes attestations by ID in batches and decrements enforcement counters.
func (h *distillHandler) deleteAndUpdateCounters(ctx context.Context, ids []string, batch []*types.As) (int, error) {
	deleteStart := time.Now()
	// Batch delete using IN clauses (chunks of 500 to stay within SQLite limits)
	deleted := 0
	const chunkSize = 500
	for i := 0; i < len(ids); i += chunkSize {
		end := i + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		placeholders := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for j, id := range chunk {
			placeholders[j] = "?"
			args[j] = id
		}
		query := "DELETE FROM attestations WHERE id IN (" + strings.Join(placeholders, ",") + ")"
		result, err := h.db.ExecContext(ctx, query, args...)
		if err != nil {
			return deleted, errors.Wrapf(err, "batch delete %d attestations (chunk %d)", len(chunk), i/chunkSize)
		}
		n, _ := result.RowsAffected()
		deleted += int(n)
	}

	deleteDur := time.Since(deleteStart)

	// Rebuild enforcement counters for affected actors from actual data.
	// Instead of decrementing one-by-one (3,584 queries for 896 actors),
	// delete stale counter rows and repopulate from junction tables.
	counterStart := time.Now()

	// Collect unique actors from the deleted batch
	actorSet := make(map[string]bool)
	for _, as := range batch {
		for _, actor := range as.Actors {
			actorSet[actor] = true
		}
	}
	actors := make([]string, 0, len(actorSet))
	for a := range actorSet {
		actors = append(actors, a)
	}

	// Process in chunks to stay within SQLite parameter limits
	const actorChunkSize = 200
	for i := 0; i < len(actors); i += actorChunkSize {
		end := i + actorChunkSize
		if end > len(actors) {
			end = len(actors)
		}
		chunk := actors[i:end]

		placeholders := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for j, actor := range chunk {
			placeholders[j] = "?"
			args[j] = actor
		}
		inClause := strings.Join(placeholders, ",")

		// 1. Delete stale counter rows for these actors
		h.db.ExecContext(ctx,
			"DELETE FROM enforcement_actor_context WHERE actor IN ("+inClause+")", args...)

		// 2. Rebuild from actual attestation data
		h.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO enforcement_actor_context (actor, context, count)
			SELECT aa.actor, ac.context, COUNT(DISTINCT aa.attestation_id)
			FROM attestation_actors aa
			JOIN attestation_contexts ac ON ac.attestation_id = aa.attestation_id
			WHERE aa.actor IN (`+inClause+`)
			GROUP BY aa.actor, ac.context
		`, args...)

		// 3. Rebuild actor_contexts counts
		h.db.ExecContext(ctx,
			"DELETE FROM enforcement_actor_contexts WHERE actor IN ("+inClause+")", args...)
		h.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO enforcement_actor_contexts (actor, count)
			SELECT actor, COUNT(*) FROM enforcement_actor_context
			WHERE actor IN (`+inClause+`)
			GROUP BY actor
		`, args...)
	}

	counterDur := time.Since(counterStart)
	h.logger.Debugw("deleteAndUpdateCounters timing",
		"delete_ms", deleteDur.Milliseconds(),
		"counter_rebuild_ms", counterDur.Milliseconds(),
		"ids", len(ids),
		"actors", len(actors))

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

	// Contexts as set (capped at 50)
	contexts := setToSlice(contextSet, 50)

	// Actors: use canonical "distill" actor to avoid inflating entity_actors_limit.
	// The original actor union is preserved as _actors_sample in attributes.
	actorsSample := setToSlice(actorSet, 50)

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

	// Preserve original actor set in attributes
	merged["_actors_count"] = len(actorSet)
	merged["_actors_sample"] = actorsSample

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

	// Build temporal histogram from attestation timestamps
	merged["_histogram"] = buildHistogram(batch)

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
		Actors:     []string{"distill"},
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
	frequencies map[string]int // value → observation count
	unplaced    map[string]bool // values from old-format aggregates (no frequency data)
	count       int
}

// mergeAttributes mechanically merges attributes from a batch of attestations.
//   - Numbers: {min, max, sum, count}
//   - Strings: {frequencies: {val: count, ...}, count} (frequencies capped at 50)
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
			if k == "_distill" || k == "_count" || k == "_total" || k == "_first_seen" || k == "_last_seen" || k == "_version" || k == "_rust_version" || k == "_subjects_count" || k == "_subjects_sample" || k == "_actors_count" || k == "_actors_sample" || k == "_histogram" {
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
					agg.frequencies[val]++
					agg.count++
				} else {
					strs[k] = &distillStrAgg{
						frequencies: map[string]int{val: 1},
						unplaced:    make(map[string]bool),
						count:       1,
					}
				}
			case map[string]interface{}:
				// Already-aggregated from prior distill — merge aggregates
				if hasAggKeys(val) {
					if agg, ok := nums[k]; ok {
						mergeNumAgg(agg, val)
					} else {
						nums[k] = numAggFromMap(val)
					}
				} else if isStrAgg(val) {
					aCount := 0
					if c, ok := val["count"].(float64); ok {
						aCount = int(c)
					}
					if agg, ok := strs[k]; ok {
						agg.count += aCount
						mergeStrAgg(agg, val)
					} else {
						agg := &distillStrAgg{
							frequencies: make(map[string]int),
							unplaced:    make(map[string]bool),
							count:       aCount,
						}
						mergeStrAgg(agg, val)
						strs[k] = agg
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
		if agg.count == len(batch) && len(agg.frequencies) == 1 && len(agg.unplaced) == 0 {
			// Constant string — keep as scalar
			for v := range agg.frequencies {
				result[k] = v
			}
		} else {
			// Cap frequencies at 50 entries, keep highest
			freqMap := make(map[string]interface{})
			if len(agg.frequencies) <= 50 {
				for v, c := range agg.frequencies {
					freqMap[v] = c
				}
			} else {
				type freqEntry struct {
					val   string
					count int
				}
				entries := make([]freqEntry, 0, len(agg.frequencies))
				for v, c := range agg.frequencies {
					entries = append(entries, freqEntry{v, c})
				}
				// Sort by count descending — simple selection of top 50
				for i := 0; i < 50 && i < len(entries); i++ {
					maxIdx := i
					for j := i + 1; j < len(entries); j++ {
						if entries[j].count > entries[maxIdx].count {
							maxIdx = j
						}
					}
					entries[i], entries[maxIdx] = entries[maxIdx], entries[i]
					freqMap[entries[i].val] = entries[i].count
				}
			}

			out := map[string]interface{}{
				"frequencies": freqMap,
				"count":       agg.count,
			}
			if len(agg.unplaced) > 0 {
				unplaced := make([]string, 0, len(agg.unplaced))
				for v := range agg.unplaced {
					unplaced = append(unplaced, v)
				}
				out["unplaced"] = unplaced
			}
			result[k] = out
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

// isStrAgg checks if a map is a string aggregate ({frequencies, count} or legacy {values, count}).
func isStrAgg(m map[string]interface{}) bool {
	_, hasCount := m["count"]
	_, hasFreqs := m["frequencies"]
	_, hasValues := m["values"]
	return hasCount && (hasFreqs || hasValues)
}

// mergeStrAgg merges a string aggregate map into an existing distillStrAgg.
// New-format {frequencies: {val: count}} merges by summing frequencies.
// Old-format {values: [...]} passes values as unplaced (no fabricated counts).
func mergeStrAgg(agg *distillStrAgg, m map[string]interface{}) {
	if freqs, ok := m["frequencies"].(map[string]interface{}); ok {
		for key, val := range freqs {
			if c, ok := val.(float64); ok {
				agg.frequencies[key] += int(c)
			}
		}
	} else if vals, ok := m["values"].([]interface{}); ok {
		// Old format — values without frequencies
		for _, v := range vals {
			if s, ok := v.(string); ok {
				if _, inFreqs := agg.frequencies[s]; !inFreqs {
					agg.unplaced[s] = true
				}
			}
		}
	}
	// Merge unplaced from prior new-format aggregates
	if unplaced, ok := m["unplaced"].([]interface{}); ok {
		for _, v := range unplaced {
			if s, ok := v.(string); ok {
				if _, inFreqs := agg.frequencies[s]; !inFreqs {
					agg.unplaced[s] = true
				}
			}
		}
	}
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

const histogramKeyBudget = 200

// buildHistogram creates a temporal histogram from attestation timestamps.
// Raw attestations get bucketed into 10min keys. Existing distill attestations
// with _histogram get their histograms merged. Legacy sigmas without _histogram
// contribute nothing (their _total is still counted — sum(histogram) <= _total).
func buildHistogram(batch []*types.As) map[string]int {
	histogram := make(map[string]int)

	for _, as := range batch {
		if as.Attributes != nil {
			if existing, ok := as.Attributes["_histogram"]; ok {
				if histMap, ok := existing.(map[string]interface{}); ok {
					for key, val := range histMap {
						if count, ok := val.(float64); ok {
							histogram[key] += int(count)
						}
					}
					continue
				}
			}
		}

		// Skip distill attestations without histogram (legacy unplaced)
		if as.Attributes != nil {
			if _, ok := as.Attributes["_distill"]; ok {
				continue
			}
		}

		// Raw attestation — bucket its timestamp
		key := timestampTo10minKey(as.Timestamp)
		histogram[key]++
	}

	return coarsenHistogram(histogram)
}

// timestampTo10minKey converts a timestamp to a 10-minute bucket key.
// Format: "2026-05-13T14:10" (truncated to 10min boundary).
func timestampTo10minKey(t time.Time) string {
	t = t.UTC()
	minute := (t.Minute() / 10) * 10
	return fmt.Sprintf("%sT%02d:%02d", t.Format("2006-01-02"), t.Hour(), minute)
}

// coarsenHistogram collapses the finest tier if key count exceeds budget.
//
// Tiers by key length:
//   - 16 chars: 10min ("2026-05-13T14:10")
//   - 13 chars: hourly ("2026-05-13T14")
//   - 10 chars: daily  ("2026-05-13")
//   - 8 chars:  weekly ("2026-W20")
func coarsenHistogram(histogram map[string]int) map[string]int {
	if len(histogram) <= histogramKeyBudget {
		return histogram
	}

	// Find finest tier (longest key)
	maxLen := 0
	for key := range histogram {
		if len(key) > maxLen {
			maxLen = len(key)
		}
	}

	coarsened := make(map[string]int)
	for key, count := range histogram {
		if len(key) == maxLen {
			coarsened[coarsenKey(key)] += count
		} else {
			coarsened[key] += count
		}
	}

	if len(coarsened) > histogramKeyBudget {
		return coarsenHistogram(coarsened)
	}
	return coarsened
}

// coarsenKey collapses a histogram key to the next coarser tier.
//
//	"2026-05-13T14:10" (10min) → "2026-05-13T14" (hourly)
//	"2026-05-13T14"    (hourly) → "2026-05-13"    (daily)
//	"2026-05-13"       (daily)  → ISO week key     (weekly)
func coarsenKey(key string) string {
	switch len(key) {
	case 16:
		return key[:13] // 10min → hourly: drop ":MM"
	case 13:
		return key[:10] // hourly → daily: drop "THH"
	case 10:
		// daily → weekly
		t, err := time.Parse("2006-01-02", key)
		if err != nil {
			return key
		}
		year, week := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	default:
		return key
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
