package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/sym"
)

// compileSubscriptions converts a composition's right-direction edges into watcher subscriptions.
// AX source edges use the AX glyph's query filter. Producer (py/prompt) source edges filter on actor.
func (h *CanvasHandler) compileSubscriptions(ctx context.Context, comp *glyphstorage.CanvasComposition) error {
	store := h.watcherEngine.GetStore()

	// Re-enable any SE watchers disabled by previous compilation,
	// then delete stale meld-edge watchers. This ensures removed edges
	// don't leave downstream SE watchers permanently disabled.
	h.reEnableDownstreamSEWatchers(ctx, comp.ID)

	prefix := fmt.Sprintf("meld-edge-%s-", comp.ID)
	deleted, err := store.DeleteByPrefix(ctx, prefix)
	if err != nil {
		return errors.Wrapf(err, "failed to clean stale watchers for composition %s", comp.ID)
	}

	var created int
	for _, edge := range comp.Edges {
		if edge.Direction != "right" {
			continue
		}

		// Resolve source glyph type
		sourceGlyph, err := h.store.GetGlyph(ctx, edge.From)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve source glyph: %v", edge.From, edge.To, err)
			continue
		}

		// Resolve target glyph type
		targetGlyph, err := h.store.GetGlyph(ctx, edge.To)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve target glyph: %v", edge.From, edge.To, err)
			continue
		}

		// Resolve glyph types
		targetType := glyphSymbolToType(targetGlyph.Symbol)
		sourceType := glyphSymbolToType(sourceGlyph.Symbol)

		// SE → SE: compound semantic watcher (intersection)
		if sourceType == "semantic" && targetType == "semantic" {
			watcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, edge.From, edge.To)

			// Get upstream (source) SE glyph's query
			upstreamWatcherID := fmt.Sprintf("se-glyph-%s", edge.From)
			upstreamWatcher, err := store.Get(ctx, upstreamWatcherID)
			if err != nil {
				h.logWarn("Skipping SE→SE edge %s→%s: no watcher found for upstream %s: %v", edge.From, edge.To, upstreamWatcherID, err)
				continue
			}

			// Get downstream (target) SE glyph's query
			downstreamWatcherID := fmt.Sprintf("se-glyph-%s", edge.To)
			downstreamWatcher, err := store.Get(ctx, downstreamWatcherID)
			if err != nil {
				h.logWarn("Skipping SE→SE edge %s→%s: no watcher found for downstream %s: %v", edge.From, edge.To, downstreamWatcherID, err)
				continue
			}

			actionData, err := json.Marshal(map[string]string{
				"target_glyph_id": edge.To,
				"composition_id":  comp.ID,
				"source_glyph_id": edge.From,
			})
			if err != nil {
				return errors.Wrap(err, "failed to marshal action data")
			}

			w := &storage.Watcher{
				ID:                        watcherID,
				Name:                      fmt.Sprintf("Meld: %s → %s", truncate(edge.From, 8), truncate(edge.To, 8)),
				ActionType:                storage.ActionTypeSemanticMatch,
				ActionData:                string(actionData),
				MaxFiresPerMinute:         60,
				Enabled:                   true,
				SemanticQuery:             downstreamWatcher.SemanticQuery,
				SemanticThreshold:         downstreamWatcher.SemanticThreshold,
				SemanticClusterID:         downstreamWatcher.SemanticClusterID,
				UpstreamSemanticQuery:     upstreamWatcher.SemanticQuery,
				UpstreamSemanticThreshold: upstreamWatcher.SemanticThreshold,
			}

			if err := store.CreateOrReplace(ctx, w); err != nil {
				return errors.Wrapf(err, "failed to create SE→SE subscription for edge %s→%s", edge.From, edge.To)
			}

			// Engine-level suppression in loadWatchers handles removing the
			// standalone SE watcher from the in-memory map. No DB disable needed.

			created++
			continue
		}

		// Target must be an executable glyph type for non-SE targets
		if targetType != "py" && targetType != "prompt" {
			continue
		}

		watcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, edge.From, edge.To)
		actionData, err := json.Marshal(map[string]string{
			"target_glyph_id":   edge.To,
			"target_glyph_type": targetType,
			"composition_id":    comp.ID,
			"source_glyph_id":   edge.From,
		})
		if err != nil {
			return errors.Wrap(err, "failed to marshal action data")
		}

		w := &storage.Watcher{
			ID:                watcherID,
			Name:              fmt.Sprintf("Meld: %s → %s", truncate(edge.From, 8), truncate(edge.To, 8)),
			ActionType:        storage.ActionTypeGlyphExecute,
			ActionData:        string(actionData),
			MaxFiresPerMinute: 60,
			Enabled:           true,
		}

		// Set filter based on source glyph type
		switch sourceType {
		case "ax":
			// AX source: reuse the AX glyph's query from its existing watcher
			axWatcherID := fmt.Sprintf("ax-glyph-%s", edge.From)
			axWatcher, err := store.Get(ctx, axWatcherID)
			if err != nil {
				h.logWarn("Skipping AX edge %s→%s: no watcher found for %s: %v", edge.From, edge.To, axWatcherID, err)
				continue
			}
			w.AxQuery = axWatcher.AxQuery
		case "semantic":
			// Semantic source → executable target: reuse the ⊨ glyph's semantic query
			seWatcherID := fmt.Sprintf("se-glyph-%s", edge.From)
			seWatcher, err := store.Get(ctx, seWatcherID)
			if err != nil {
				h.logWarn("Skipping semantic edge %s→%s: no watcher found for %s: %v", edge.From, edge.To, seWatcherID, err)
				continue
			}
			w.SemanticQuery = seWatcher.SemanticQuery
			w.SemanticThreshold = seWatcher.SemanticThreshold

			// If source SE is itself a downstream target of an SE→SE meld,
			// propagate the upstream query so the engine enforces the full
			// intersection before executing the downstream glyph.
			compoundWatchers, err := store.FindCompoundWatchersForTarget(ctx, edge.From)
			if err == nil && len(compoundWatchers) > 0 {
				w.UpstreamSemanticQuery = compoundWatchers[0].UpstreamSemanticQuery
				w.UpstreamSemanticThreshold = compoundWatchers[0].UpstreamSemanticThreshold
			}
		case "py", "prompt":
			// Producer source: filter on attestations created by the upstream glyph
			w.Filter.Actors = []string{fmt.Sprintf("glyph:%s", edge.From)}
		default:
			h.logWarn("Skipping edge %s→%s: unsupported source type %s", edge.From, edge.To, sourceType)
			continue
		}

		if err := store.CreateOrReplace(ctx, w); err != nil {
			return errors.Wrapf(err, "failed to create subscription for edge %s→%s", edge.From, edge.To)
		}
		created++
	}

	if created > 0 || deleted > 0 {
		if err := h.watcherEngine.ReloadWatchers(); err != nil {
			return errors.Wrap(err, "failed to reload watchers after subscription compilation")
		}
		h.logInfo("Compiled %d subscriptions for composition %s (cleaned %d stale)", created, comp.ID, deleted)
	}

	return nil
}

// reEnableDownstreamSEWatchers logs which SE watchers will be restored when a
// composition containing SE→SE meld edges is deleted. The actual restoration
// happens via ReloadWatchers() after the compound meld-edge watcher is deleted
// from the DB — the suppression loop in loadWatchers no longer finds a compound
// watcher targeting the SE glyph, so the standalone SE watcher re-enters the
// in-memory map naturally. No DB update is needed because engine-level
// suppression never sets Enabled=false in the DB.
func (h *CanvasHandler) reEnableDownstreamSEWatchers(ctx context.Context, compositionID string) {
	store := h.watcherEngine.GetStore()
	watchers, err := store.List(ctx, false)
	if err != nil {
		h.logWarn("Failed to list watchers for SE re-enable: %v", err)
		return
	}

	prefix := fmt.Sprintf("meld-edge-%s-", compositionID)
	for _, w := range watchers {
		if w.ActionType != storage.ActionTypeSemanticMatch || !strings.HasPrefix(w.ID, prefix) {
			continue
		}
		var actionData struct {
			TargetGlyphID string `json:"target_glyph_id"`
		}
		if err := json.Unmarshal([]byte(w.ActionData), &actionData); err != nil {
			h.logWarn("Failed to unmarshal action data for meld-edge watcher %s: %v", w.ID, err)
			continue
		}
		if actionData.TargetGlyphID == "" {
			continue
		}
		h.logInfo("SE watcher se-glyph-%s will be restored after unmeld (compound %s removed)", actionData.TargetGlyphID, w.ID)
	}
}

// glyphSymbolToType maps glyph symbol to short type name for subscription logic.
// Symbols come from the sym package or are stored as literal strings (e.g. "py").
func glyphSymbolToType(symbol string) string {
	switch symbol {
	case "py":
		return "py"
	case sym.AX: // ⋈
		return "ax"
	case sym.SE: // ⊨ — semantic search glyph
		return "semantic"
	case sym.SO: // ⟶ — prompt glyph uses SO symbol
		return "prompt"
	default:
		return symbol
	}
}
