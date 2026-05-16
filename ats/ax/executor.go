package ax

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// AxExecutor executes ask queries against attestation storage
type AxExecutor struct {
	queryStore     ats.AttestationQueryStore
	classifier     Classifier
	aliasResolver  *alias.Resolver
	entityResolver ats.EntityResolver
	queryExpander  ats.QueryExpander
	logger         *zap.SugaredLogger
}

// NewAxExecutor creates a new ask executor with default components:
// - Smart classification enabled
// - NoOpEntityResolver (no external identity resolution)
// - NoOpQueryExpander (literal query matching only)
func NewAxExecutor(queryStore ats.AttestationQueryStore, aliasResolver *alias.Resolver) *AxExecutor {
	return NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{})
}

// AxExecutorOptions provides optional configuration for AxExecutor.
type AxExecutorOptions struct {
	EntityResolver ats.EntityResolver // Optional entity ID resolution (default: NoOpEntityResolver)
	QueryExpander  ats.QueryExpander  // Optional query expansion (default: NoOpQueryExpander)
	Logger         *zap.SugaredLogger // Optional logger for debug output (default: nil, no logging)
	RawQuerier     interface{}        // Optional: routes attestation queries through Rust FFI (storage.RawQuerier)
}

// NewAxExecutorWithOptions creates an executor with custom options.
func NewAxExecutorWithOptions(queryStore ats.AttestationQueryStore, aliasResolver *alias.Resolver, opts AxExecutorOptions) *AxExecutor {
	// Set defaults for nil options
	if opts.EntityResolver == nil {
		opts.EntityResolver = &ats.NoOpEntityResolver{}
	}
	if opts.QueryExpander == nil {
		opts.QueryExpander = &ats.NoOpQueryExpander{}
	}
	return &AxExecutor{
		queryStore:     queryStore,
		classifier:     NewDefaultClassifier(DefaultTemporalConfig()),
		aliasResolver:  aliasResolver,
		entityResolver: opts.EntityResolver,
		queryExpander:  opts.QueryExpander,
		logger:         opts.Logger,
	}
}

// SetClassificationConfig replaces the classifier with one using the provided config
func (ae *AxExecutor) SetClassificationConfig(config TemporalConfig) {
	ae.classifier = NewDefaultClassifier(config)
}

// ExecuteAsk executes an ask query and returns results
func (ae *AxExecutor) ExecuteAsk(ctx context.Context, filter types.AxFilter) (*types.AxResult, error) {
	startTime := time.Now()

	if ae.logger != nil {
		ae.logger.Debugw("executing ax query",
			"subjects", filter.Subjects,
			"predicates", filter.Predicates,
			"contexts", filter.Contexts,
		)
	}

	result := &types.AxResult{
		Attestations: []types.As{},
		Conflicts:    []types.Conflict{}, // Empty for now - defer to Phase 2.4
		Debug: types.AxDebug{
			OriginalFilter: filter,
		},
	}

	// 1. Resolve aliases for all filter components
	expandedFilter, err := ae.expandAliasesInFilter(ctx, filter)
	if err != nil {
		err = errors.Wrap(err, "failed to expand aliases in filter")
		err = errors.WithDetail(err, fmt.Sprintf("Original subjects: %v", filter.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Original predicates: %v", filter.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Original contexts: %v", filter.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Original actors: %v", filter.Actors))
		return nil, err
	}

	// Predicates and contexts are matched literally. Fuzzy expansion was removed;
	// MeiliSearch via the qntx-meili plugin (ADR-015) will provide search.

	// Store debug information
	result.Debug.ExpandedFilter = expandedFilter

	// 3. Execute query using query store
	attestationsPtr, err := ae.queryStore.ExecuteAxQuery(ctx, expandedFilter)
	if err != nil {
		err = errors.Wrap(err, "failed to execute query")
		err = errors.WithDetail(err, fmt.Sprintf("Expanded subjects: %v", expandedFilter.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", expandedFilter.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", expandedFilter.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", expandedFilter.Actors))
		err = errors.WithDetail(err, fmt.Sprintf("Limit: %d", expandedFilter.Limit))
		return nil, err
	}

	// Convert []*As to []As
	attestations := make([]types.As, len(attestationsPtr))
	for i, as := range attestationsPtr {
		attestations[i] = *as
	}

	// 5. Apply cartesian expansion
	claims := ats.ExpandCartesianClaims(attestations)

	// 6. Conflict detection with smart classification
	result.Conflicts, result.Attestations = ae.executeAdvancedClassification(claims)

	// 7. Generate summary
	result.Summary = ae.generateSummary(result.Attestations)

	// Record execution time
	result.Debug.ExecutionTimeMs = time.Since(startTime).Milliseconds()

	return result, nil
}

// executeAdvancedClassification groups claims, classifies conflicts, applies resolution
// strategies, and returns deterministically ordered results.
//
// Ordering contract: results are sorted by confidence desc, then recency desc.
// Classified conflicts carry their computed confidence; unclassified claims (no conflict)
// get a neutral 0.5 — present but uncorroborated.
func (ae *AxExecutor) executeAdvancedClassification(claims []ats.IndividualClaim) ([]types.Conflict, []types.As) {
	// Group claims by key for classification
	claimGroups := make(map[string][]ats.IndividualClaim)

	for _, claim := range claims {
		key := claim.Subject + "|" + claim.Predicate + "|" + claim.Context + "|" + claim.Actor
		claimGroups[key] = append(claimGroups[key], claim)
	}

	// Perform smart classification
	classificationResult := ae.classifier.ClassifyConflicts(claimGroups)

	// Apply resolution strategies to filter claims
	filteredClaims := ae.applyResolutionStrategies(claimGroups, classificationResult.Conflicts)

	// Build confidence lookup from classification results
	confidenceMap := make(map[string]float64)
	for _, conflict := range classificationResult.Conflicts {
		key := conflict.Conflict.Subject + "|" + conflict.Conflict.Predicate + "|" + conflict.Conflict.Context
		confidenceMap[key] = conflict.Confidence
	}

	// Sort by confidence desc, then recency desc, then source ID for deterministic ordering
	sort.Slice(filteredClaims, func(i, j int) bool {
		ci := claimConfidence(filteredClaims[i], confidenceMap)
		cj := claimConfidence(filteredClaims[j], confidenceMap)
		if ci != cj {
			return ci > cj
		}
		if !filteredClaims[i].Timestamp.Equal(filteredClaims[j].Timestamp) {
			return filteredClaims[i].Timestamp.After(filteredClaims[j].Timestamp)
		}
		return filteredClaims[i].SourceAs.ID < filteredClaims[j].SourceAs.ID
	})

	// Convert AdvancedConflicts back to basic Conflicts
	var conflicts []types.Conflict
	for _, advancedConflict := range classificationResult.Conflicts {
		conflicts = append(conflicts, advancedConflict.Conflict)
	}

	// Convert filtered claims back to attestations (preserves sorted order via first-seen dedup)
	attestations := ats.ConvertClaimsToAttestations(filteredClaims)

	return conflicts, attestations
}

// applyResolutionStrategies applies resolution strategies to filter claims based on classification results
func (ae *AxExecutor) applyResolutionStrategies(claimGroups map[string][]ats.IndividualClaim, conflicts []AdvancedConflict) []ats.IndividualClaim {
	// Create a map of conflict resolutions by matching conflicts to claim groups
	resolutionMap := make(map[string]AdvancedConflict)
	for _, conflict := range conflicts {
		// Find the matching claim group for this conflict
		for groupKey, groupClaims := range claimGroups {
			if len(groupClaims) > 0 {
				firstClaim := groupClaims[0]
				if firstClaim.Subject == conflict.Conflict.Subject &&
					firstClaim.Predicate == conflict.Conflict.Predicate &&
					firstClaim.Context == conflict.Conflict.Context {
					resolutionMap[groupKey] = conflict
					break
				}
			}
		}
	}

	var filteredClaims []ats.IndividualClaim

	// Process each claim group
	for groupKey, groupClaims := range claimGroups {
		if len(groupClaims) <= 1 {
			// Single claim - always include
			filteredClaims = append(filteredClaims, groupClaims...)
			continue
		}

		// Check if this group has a conflict resolution
		if conflict, hasConflict := resolutionMap[groupKey]; hasConflict {
			// Apply strategy based on resolution type
			switch conflict.Strategy {
			case "show_latest":
				// Evolution - show only the most recent claim
				latest := ae.getMostRecentClaim(groupClaims)
				filteredClaims = append(filteredClaims, latest)
			case "show_all_sources":
				// Verification - show all sources (no filtering)
				filteredClaims = append(filteredClaims, groupClaims...)
			case "show_highest_authority":
				// Supersession - show only the highest authority claim
				highest := ae.getHighestAuthorityClaim(groupClaims)
				filteredClaims = append(filteredClaims, highest)
			case "show_all_contexts":
				// Coexistence - show all (different contexts should coexist)
				filteredClaims = append(filteredClaims, groupClaims...)
			default:
				// Unknown strategy or requires review - show all for human decision
				filteredClaims = append(filteredClaims, groupClaims...)
			}
		} else {
			// No conflict detected - show all claims
			filteredClaims = append(filteredClaims, groupClaims...)
		}
	}

	return filteredClaims
}

// getMostRecentClaim returns the claim with the most recent timestamp
func (ae *AxExecutor) getMostRecentClaim(claims []ats.IndividualClaim) ats.IndividualClaim {
	if len(claims) == 0 {
		return ats.IndividualClaim{}
	}

	mostRecent := claims[0]
	for _, claim := range claims[1:] {
		if claim.Timestamp.After(mostRecent.Timestamp) {
			mostRecent = claim
		}
	}
	return mostRecent
}

// getHighestAuthorityClaim returns the claim from the highest authority actor.
// Uses Rust's actor prefix convention: human: > llm: > system: > external.
func (ae *AxExecutor) getHighestAuthorityClaim(claims []ats.IndividualClaim) ats.IndividualClaim {
	if len(claims) == 0 {
		return ats.IndividualClaim{}
	}

	best := claims[0]
	bestRank := actorRank(best.Actor)
	for _, claim := range claims[1:] {
		r := actorRank(claim.Actor)
		if r > bestRank {
			best = claim
			bestRank = r
		}
	}
	return best
}

// actorRank returns a numeric rank matching Rust's ActorCredibility ordering:
// Human(3) > Llm(2) > System(1) > External(0)
func actorRank(actor string) int {
	lower := strings.ToLower(actor)
	if strings.HasPrefix(lower, "human:") || strings.HasSuffix(lower, "@verified") {
		return 3
	}
	if strings.HasPrefix(lower, "llm:") || strings.Contains(lower, "gpt") || strings.Contains(lower, "claude") || strings.Contains(lower, "anthropic") || strings.Contains(lower, "openai") {
		return 2
	}
	if strings.HasPrefix(lower, "system:") || strings.HasPrefix(lower, "qntx:") {
		return 1
	}
	return 0
}

// claimConfidence returns the confidence for a claim based on its conflict group.
// Unclassified claims get 0.5 — present but uncorroborated.
func claimConfidence(claim ats.IndividualClaim, confidenceMap map[string]float64) float64 {
	key := claim.Subject + "|" + claim.Predicate + "|" + claim.Context
	if conf, ok := confidenceMap[key]; ok {
		return conf
	}
	return 0.5
}

// generateSummary generates a basic summary of the results
func (ae *AxExecutor) generateSummary(attestations []types.As) types.AxSummary {
	// Simplified summary - just basic counts
	summary := types.AxSummary{
		TotalAttestations: len(attestations),
		UniqueSubjects:    make(map[string]int),
		UniquePredicates:  make(map[string]int),
		UniqueContexts:    make(map[string]int),
		UniqueActors:      make(map[string]int),
	}

	// Basic counting - defer complex analysis to later phases
	for _, as := range attestations {
		// Count subjects
		for _, subject := range as.Subjects {
			summary.UniqueSubjects[subject]++
		}

		// Count predicates
		for _, predicate := range as.Predicates {
			if predicate != "_" {
				summary.UniquePredicates[predicate]++
			}
		}

		// Count contexts
		for _, context := range as.Contexts {
			if context != "_" {
				summary.UniqueContexts[context]++
			}
		}

		// Count actors
		for _, actor := range as.Actors {
			summary.UniqueActors[actor]++
		}
	}

	return summary
}

// expandAliasesInFilter expands all identifiers in the filter using dual storage alias resolution
// This includes both AS alias system and Contact RLDB alternative IDs for unified entity resolution
func (ae *AxExecutor) expandAliasesInFilter(ctx context.Context, filter types.AxFilter) (types.AxFilter, error) {
	expandedFilter := filter // Copy the filter

	// Expand subjects with dual storage support
	if len(filter.Subjects) > 0 {
		var expandedSubjects []string
		for _, subject := range filter.Subjects {
			// Get identifiers from both alias system and Contact RLDB alternative IDs
			allIdentifiers, err := ae.getUnifiedIdentifiers(ctx, subject)
			if err != nil {
				err = errors.Wrapf(err, "failed to resolve subject identifiers %s", subject)
				err = errors.WithDetail(err, fmt.Sprintf("Subject: %s", subject))
				err = errors.WithDetail(err, "Filter component: subjects")
				return filter, err
			}
			expandedSubjects = append(expandedSubjects, allIdentifiers...)
		}
		expandedFilter.Subjects = removeDuplicates(expandedSubjects)
	}

	// Expand contexts (use existing alias system only - contexts don't have Contact RLDB entries)
	if len(filter.Contexts) > 0 {
		var expandedContexts []string
		for _, context := range filter.Contexts {
			resolved, err := ae.aliasResolver.ResolveIdentifier(ctx, context)
			if err != nil {
				err = errors.Wrapf(err, "failed to resolve context alias %s", context)
				err = errors.WithDetail(err, fmt.Sprintf("Context: %s", context))
				err = errors.WithDetail(err, "Filter component: contexts")
				return filter, err
			}
			expandedContexts = append(expandedContexts, resolved...)
		}
		expandedFilter.Contexts = removeDuplicates(expandedContexts)
	}

	// Expand actors with dual storage support
	if len(filter.Actors) > 0 {
		var expandedActors []string
		for _, actor := range filter.Actors {
			// Get identifiers from both alias system and Contact RLDB alternative IDs
			allIdentifiers, err := ae.getUnifiedIdentifiers(ctx, actor)
			if err != nil {
				err = errors.Wrapf(err, "failed to resolve actor identifiers %s", actor)
				err = errors.WithDetail(err, fmt.Sprintf("Actor: %s", actor))
				err = errors.WithDetail(err, "Filter component: actors")
				return filter, err
			}
			expandedActors = append(expandedActors, allIdentifiers...)
		}
		expandedFilter.Actors = removeDuplicates(expandedActors)
	}

	// Predicates are not alias-expanded — they are matched literally

	return expandedFilter, nil
}

// getUnifiedIdentifiers retrieves all identifiers for an entity from both AS alias system and EntityResolver.
// This implements dual storage deduplication by ensuring all alternative IDs are included in queries.
func (ae *AxExecutor) getUnifiedIdentifiers(ctx context.Context, identifier string) ([]string, error) {
	allIdentifiers := make(map[string]bool)

	// Always include the original identifier
	allIdentifiers[identifier] = true

	// 1. Get identifiers from AS alias system
	aliasResolved, err := ae.aliasResolver.ResolveIdentifier(ctx, identifier)
	if err != nil {
		err = errors.Wrapf(err, "failed to resolve AS aliases for %s", identifier)
		err = errors.WithDetail(err, fmt.Sprintf("Identifier: %s", identifier))
		err = errors.WithDetail(err, "Operation: AS alias resolution")
		return nil, err
	}
	for _, resolved := range aliasResolved {
		allIdentifiers[resolved] = true
	}

	// 2. Get alternative IDs from EntityResolver (if configured)
	if ae.entityResolver != nil {
		alternativeIDs, err := ae.entityResolver.GetAlternativeIDs(identifier)
		if err != nil {
			err = errors.Wrapf(err, "failed to resolve entity alternatives for %s", identifier)
			err = errors.WithDetail(err, fmt.Sprintf("Identifier: %s", identifier))
			err = errors.WithDetail(err, "Operation: Entity resolver lookup")
			return nil, err
		}
		for _, altID := range alternativeIDs {
			allIdentifiers[altID] = true
		}
	}

	// Convert map keys to slice
	var result []string
	for id := range allIdentifiers {
		result = append(result, id)
	}

	return result, nil
}
