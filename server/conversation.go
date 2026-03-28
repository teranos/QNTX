package server

import (
	"context"
	"sort"

	"github.com/teranos/QNTX/ai/provider"
	"github.com/teranos/QNTX/ats/types"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
)

// ConversationAssembler builds a multi-turn message history from the
// canvas composition graph. Given a glyph ID, it traces upstream melds
// to find prior prompt-result attestations and assembles them as an
// ordered message array for the LLM.
type ConversationAssembler struct {
	canvasStore *glyphstorage.CanvasStore
	queryStore  queryStore
}

// queryStore is the subset of AttestationQueryStore we need.
type queryStore interface {
	ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error)
}

// NewConversationAssembler creates a new assembler.
func NewConversationAssembler(cs *glyphstorage.CanvasStore, qs queryStore) *ConversationAssembler {
	return &ConversationAssembler{canvasStore: cs, queryStore: qs}
}

// AssembleMessages traces the meld graph upstream from glyphID and
// builds an ordered message history. Returns nil if no history found.
func (a *ConversationAssembler) AssembleMessages(ctx context.Context, glyphID string) ([]provider.Message, error) {
	// Find the composition containing this glyph
	compositions, err := a.canvasStore.ListCompositions(ctx)
	if err != nil {
		return nil, err
	}

	// Find which composition this glyph belongs to
	var comp *glyphstorage.CanvasComposition
	for _, c := range compositions {
		for _, e := range c.Edges {
			if e.From == glyphID || e.To == glyphID {
				comp = c
				break
			}
		}
		if comp != nil {
			break
		}
	}

	if comp == nil {
		// No composition — single glyph, no history
		return nil, nil
	}

	// Collect all glyph IDs upstream of glyphID by walking edges backwards.
	// The target glyph is the "To" side of edges; walk "From" to find parents.
	upstreamIDs := collectUpstream(comp, glyphID)

	if len(upstreamIDs) == 0 {
		return nil, nil
	}

	// Query prompt-result attestations for each upstream glyph
	var allResults []*types.As
	for _, id := range upstreamIDs {
		filter := types.AxFilter{
			Contexts:   []string{id},
			Predicates: []string{"prompt-result"},
			Limit:      10,
		}
		results, err := a.queryStore.ExecuteAxQuery(ctx, filter)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}

	// Sort by timestamp ascending — conversation order
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Timestamp.Before(allResults[j].Timestamp)
	})

	// Build messages from attestation pairs (template=user, response=assistant)
	var messages []provider.Message
	for _, as := range allResults {
		template, _ := as.Attributes["template"].(string)
		response, _ := as.Attributes["response"].(string)

		if template != "" {
			messages = append(messages, provider.NewTextMessage("user", template))
		}
		if response != "" {
			messages = append(messages, provider.NewTextMessage("assistant", response))
		}
	}

	return messages, nil
}

// collectUpstream walks the composition DAG backwards from targetID,
// collecting all glyph IDs that are upstream (ancestors).
// Returns IDs in topological order (parents before children).
func collectUpstream(comp *glyphstorage.CanvasComposition, targetID string) []string {
	// Build adjacency: for each glyph, what are its parents?
	parents := make(map[string][]string)
	for _, e := range comp.Edges {
		parents[e.To] = append(parents[e.To], e.From)
	}

	// BFS backwards from target
	visited := make(map[string]bool)
	queue := []string{targetID}
	var result []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true

		// Don't include the target itself — we want upstream only
		if current != targetID {
			result = append(result, current)
		}

		for _, p := range parents[current] {
			if !visited[p] {
				queue = append(queue, p)
			}
		}
	}

	// Reverse so parents come first (topological)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}
