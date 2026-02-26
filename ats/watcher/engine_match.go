package watcher

import (
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// OnAttestationCreated is called when a new attestation is created.
// Handles structural watchers only — semantic watchers are handled by
// OnAttestationEmbedded after the embedding observer generates the vector.
func (e *Engine) OnAttestationCreated(as *types.As) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, watcher := range e.watchers {
		if !watcher.Enabled {
			continue
		}

		// Skip semantic watchers — handled by OnAttestationEmbedded
		// to avoid redundant GenerateEmbedding FFI calls.
		if watcher.SemanticQuery != "" {
			continue
		}

		matched, score := e.matchesWatcher(as, watcher)
		if !matched {
			continue
		}

		// Broadcast match to frontend (for live results display)
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcher.ID, as, score)
		}

		// Check rate limit for action execution
		// Per QNTX LAW: "Zero means zero" - if MaxFiresPerSecond is 0, never execute
		if watcher.MaxFiresPerSecond == 0 {
			continue
		}
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.enqueueAttestation(watcher.ID, as, "rate_limited", 0, "")
			continue
		}

		// Execute async with a deep copy to prevent race conditions
		asCopy := deepCopyAttestation(as)
		go e.executeAction(watcher, asCopy)
	}
}

// OnAttestationEmbedded is called by the embedding observer after an attestation
// has been embedded and stored. It handles semantic watcher matching using the
// pre-computed attestation embedding, avoiding redundant GenerateEmbedding FFI calls.
func (e *Engine) OnAttestationEmbedded(as *types.As, attestationEmbedding []float32) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.embeddingService == nil {
		return
	}

	for _, watcher := range e.watchers {
		if !watcher.Enabled {
			continue
		}

		// Only process semantic watchers here
		if watcher.SemanticQuery == "" {
			continue
		}

		// Structural filter must also pass (semantic + structural are ANDed)
		if !e.matchesFilter(as, watcher) {
			continue
		}

		// Get cached query embedding for this watcher
		queryEmbedding := e.queryEmbeddings[watcher.ID]
		if queryEmbedding == nil {
			continue
		}

		// Compute cosine similarity (cheap — pure math, no FFI)
		similarity, err := e.embeddingService.ComputeSimilarity(queryEmbedding, attestationEmbedding)
		if err != nil {
			e.logger.Debugw("Failed to compute similarity",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}

		threshold := watcher.SemanticThreshold
		if threshold <= 0 {
			threshold = 0.3
		}

		if similarity < threshold {
			continue
		}

		// Compound SE→SE watcher: upstream must also pass
		if watcher.UpstreamSemanticQuery != "" {
			upstreamEmbedding := e.queryEmbeddings[watcher.ID+":upstream"]
			if upstreamEmbedding == nil {
				continue
			}
			upstreamSimilarity, err := e.embeddingService.ComputeSimilarity(upstreamEmbedding, attestationEmbedding)
			if err != nil {
				e.logger.Debugw("Failed to compute upstream similarity",
					"watcher_id", watcher.ID,
					"attestation_id", as.ID,
					"error", err)
				continue
			}
			upstreamThreshold := watcher.UpstreamSemanticThreshold
			if upstreamThreshold <= 0 {
				upstreamThreshold = 0.3
			}
			if upstreamSimilarity < upstreamThreshold {
				continue
			}
			e.logger.Debugw("Compound semantic match (both queries pass)",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"downstream_similarity", similarity,
				"upstream_similarity", upstreamSimilarity)
		} else {
			e.logger.Debugw("Semantic match via pre-computed embedding",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"similarity", similarity,
				"threshold", threshold)
		}

		// Broadcast match to frontend (downstream similarity as score)
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcher.ID, as, similarity)
		}

		// Rate-limited action execution (same logic as OnAttestationCreated)
		if watcher.MaxFiresPerSecond == 0 {
			continue
		}
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.enqueueAttestation(watcher.ID, as, "rate_limited", 0, "")
			continue
		}

		asCopy := deepCopyAttestation(as)
		go e.executeAction(watcher, asCopy)
	}
}

// matchesWatcher checks if an attestation matches a watcher using the appropriate strategy.
// Structural filters (AxQuery / Filter fields) and semantic queries are ANDed:
// if both are set, both must pass.
// Returns (matched, similarity score). Score is 0 for structural-only matches.
func (e *Engine) matchesWatcher(as *types.As, watcher *storage.Watcher) (bool, float32) {
	// Structural filter check (empty filter passes all)
	if !e.matchesFilter(as, watcher) {
		return false, 0
	}

	// Semantic check — only if this watcher has a query embedding cached
	if _, hasSemantic := e.queryEmbeddings[watcher.ID]; hasSemantic {
		return e.matchesSemantic(as, watcher)
	}

	// No semantic embedding cached — try lazy initialization if embedding service
	// is now available (it may have been nil at loadWatchers time).
	// NOTE: called under RLock — schedule async caching, use one-shot embedding for this match.
	if watcher.SemanticQuery != "" {
		if e.embeddingService != nil {
			embedding, err := e.embeddingService.GenerateEmbedding(watcher.SemanticQuery)
			if err == nil {
				// Cache for future calls (needs write lock — do async to avoid deadlock)
				go func(id string, emb []float32) {
					e.mu.Lock()
					e.queryEmbeddings[id] = emb
					e.mu.Unlock()
					e.logger.Infow("Lazy-initialized query embedding for semantic watcher",
						"watcher_id", id)
				}(watcher.ID, embedding)
				// Use the embedding for this match immediately
				return e.matchesSemanticWithEmbedding(as, watcher, embedding)
			}
			e.logger.Debugw("Lazy embedding generation failed for semantic watcher",
				"watcher_id", watcher.ID,
				"semantic_query", watcher.SemanticQuery,
				"error", err)
		}
		return false, 0
	}

	return true, 0
}

// matchesFilter checks if an attestation matches a watcher's filter using exact field matching
func (e *Engine) matchesFilter(as *types.As, watcher *storage.Watcher) bool {
	filter := &watcher.Filter

	// Empty filter = match all
	if len(filter.Subjects) > 0 && !hasOverlap(filter.Subjects, as.Subjects) {
		return false
	}
	if len(filter.Predicates) > 0 && !hasOverlap(filter.Predicates, as.Predicates) {
		return false
	}
	if len(filter.Contexts) > 0 && !hasOverlap(filter.Contexts, as.Contexts) {
		return false
	}
	if len(filter.Actors) > 0 && !hasOverlap(filter.Actors, as.Actors) {
		return false
	}
	if filter.TimeStart != nil && as.Timestamp.Before(*filter.TimeStart) {
		return false
	}
	if filter.TimeEnd != nil && as.Timestamp.After(*filter.TimeEnd) {
		return false
	}
	return true
}

// matchesSemantic checks using the cached query embedding.
func (e *Engine) matchesSemantic(as *types.As, watcher *storage.Watcher) (bool, float32) {
	queryEmbedding := e.queryEmbeddings[watcher.ID]
	if queryEmbedding == nil {
		return false, 0
	}
	return e.matchesSemanticWithEmbedding(as, watcher, queryEmbedding)
}

// matchesSemanticWithEmbedding checks if an attestation's text content is semantically
// similar to a given query embedding. Used by both cached and lazy-init paths.
func (e *Engine) matchesSemanticWithEmbedding(as *types.As, watcher *storage.Watcher, queryEmbedding []float32) (bool, float32) {
	if e.embeddingService == nil {
		return false, 0
	}

	text := extractAttestationText(as)
	if text == "" {
		return false, 0
	}

	attestationEmbedding, err := e.embeddingService.GenerateEmbedding(text)
	if err != nil {
		e.logger.Debugw("Failed to generate embedding for attestation",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
		return false, 0
	}

	similarity, err := e.embeddingService.ComputeSimilarity(queryEmbedding, attestationEmbedding)
	if err != nil {
		e.logger.Debugw("Failed to compute similarity",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
		return false, 0
	}

	threshold := watcher.SemanticThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default threshold
	}

	matches := similarity >= threshold
	e.logger.Debugw("Semantic comparison",
		"watcher_id", watcher.ID,
		"attestation_id", as.ID,
		"similarity", similarity,
		"threshold", threshold,
		"matches", matches)

	return matches, similarity
}

// extractAttestationText returns rich text from an attestation's attributes.
// Returns empty string for structural-only attestations — semantic search
// only applies to attestations with rich text content.
// Skips metadata keys (rich_string_fields) that contain field names, not content.
func extractAttestationText(as *types.As) string {
	if as.Attributes == nil {
		return ""
	}

	var parts []string
	for key, value := range as.Attributes {
		if key == "rich_string_fields" {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok && str != "" {
					parts = append(parts, str)
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// hasOverlap returns true if there's any overlap between two string slices
func hasOverlap(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, v := range a {
		set[strings.ToLower(v)] = true
	}
	for _, v := range b {
		if set[strings.ToLower(v)] {
			return true
		}
	}
	return false
}
