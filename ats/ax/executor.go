package ax

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax/classification"
	"github.com/teranos/QNTX/ats/types"
)

// AxExecutor executes ask queries against attestation storage
type AxExecutor struct {
	queryStore     ats.AttestationQueryStore
	fuzzy          *FuzzyMatcher
	classifier     *classification.SmartClassifier
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
		fuzzy:          NewFuzzyMatcher(),
		classifier:     classification.NewSmartClassifier(classification.DefaultTemporalConfig()),
		aliasResolver:  aliasResolver,
		entityResolver: opts.EntityResolver,
		queryExpander:  opts.QueryExpander,
		logger:         opts.Logger,
	}
}

// SetClassificationConfig replaces the classifier with one using the provided config
func (ae *AxExecutor) SetClassificationConfig(config classification.TemporalConfig) {
	ae.classifier = classification.NewSmartClassifier(config)
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
		return nil, fmt.Errorf("failed to expand aliases in filter: %w", err)
	}

	// 2. Check if this might be a natural language query
	// NL queries have multiple predicates (first is NL verb, rest are context values)
	// Skip fuzzy matching for NL queries to preserve the predicate-context structure
	isPotentiallyNLQuery := len(expandedFilter.Predicates) > 1 && len(expandedFilter.Contexts) == 0

	// 2. Expand fuzzy predicates if any (skip for NL queries)
	expandedPredicates := expandedFilter.Predicates
	if len(expandedFilter.Predicates) > 0 && !isPotentiallyNLQuery {
		var err error
		expandedPredicates, err = ae.expandFuzzyPredicates(ctx, expandedFilter.Predicates)
		if err != nil {
			return nil, fmt.Errorf("failed to expand fuzzy predicates: %w", err)
		}
	}

	// 2.5. Expand fuzzy contexts if any (skip for NL queries)
	expandedContexts := expandedFilter.Contexts
	if len(expandedFilter.Contexts) > 0 && !isPotentiallyNLQuery {
		var err error
		expandedContexts, err = ae.expandFuzzyContexts(ctx, expandedFilter.Contexts)
		if err != nil {
			return nil, fmt.Errorf("failed to expand fuzzy contexts: %w", err)
		}
	}

	// Update filter with expanded contexts and predicates
	expandedFilter.Contexts = expandedContexts
	expandedFilter.Predicates = expandedPredicates

	// Store debug information
	result.Debug.ExpandedFilter = expandedFilter

	// 3. Execute query using query store
	attestationsPtr, err := ae.queryStore.ExecuteAxQuery(ctx, expandedFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
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

// executeAdvancedClassification performs smart conflict classification
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

	// Convert AdvancedConflicts back to basic Conflicts
	var conflicts []types.Conflict
	for _, advancedConflict := range classificationResult.Conflicts {
		conflicts = append(conflicts, advancedConflict.Conflict)
	}

	// Convert filtered claims back to attestations
	attestations := ats.ConvertClaimsToAttestations(filteredClaims)

	return conflicts, attestations
}

// applyResolutionStrategies applies resolution strategies to filter claims based on classification results
func (ae *AxExecutor) applyResolutionStrategies(claimGroups map[string][]ats.IndividualClaim, conflicts []classification.AdvancedConflict) []ats.IndividualClaim {
	// Create a map of conflict resolutions by matching conflicts to claim groups
	resolutionMap := make(map[string]classification.AdvancedConflict)
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

// getHighestAuthorityClaim returns the claim from the highest authority actor
func (ae *AxExecutor) getHighestAuthorityClaim(claims []ats.IndividualClaim) ats.IndividualClaim {
	if len(claims) == 0 {
		return ats.IndividualClaim{}
	}

	var actors []string
	for _, claim := range claims {
		actors = append(actors, claim.Actor)
	}

	highestCred := ae.classifier.GetHighestCredibility(actors)

	// Find claim from highest credibility actor
	for _, claim := range claims {
		if ae.classifier.GetActorCredibility(claim.Actor).Authority == highestCred.Authority {
			return claim
		}
	}

	// Fallback to first claim if no match found
	return claims[0]
}

// expandFuzzyPredicates expands query predicates using fuzzy matching
func (ae *AxExecutor) expandFuzzyPredicates(ctx context.Context, queryPredicates []string) ([]string, error) {
	if len(queryPredicates) == 0 {
		return []string{}, nil
	}

	// Get all unique predicates from database
	allPredicates, err := ae.queryStore.GetAllPredicates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get predicates from database: %w", err)
	}

	expanded := []string{}
	for _, queryPred := range queryPredicates {
		matches := ae.fuzzy.FindMatches(queryPred, allPredicates)
		expanded = append(expanded, matches...)
	}

	// Remove duplicates
	return removeDuplicates(expanded), nil
}

// expandFuzzyContexts expands query contexts using fuzzy matching
// NOTE: Basic implementation - see GitHub issue #32 for advanced matching plans
func (ae *AxExecutor) expandFuzzyContexts(ctx context.Context, queryContexts []string) ([]string, error) {
	if len(queryContexts) == 0 {
		return []string{}, nil
	}

	// Get all unique contexts from database
	allContexts, err := ae.queryStore.GetAllContexts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get contexts from database: %w", err)
	}

	expanded := []string{}
	for _, queryContext := range queryContexts {
		matches := ae.fuzzy.FindContextMatches(queryContext, allContexts)
		expanded = append(expanded, matches...)
	}

	// Remove duplicates
	return removeDuplicates(expanded), nil
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
				return filter, fmt.Errorf("failed to resolve subject identifiers %s: %w", subject, err)
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
				return filter, fmt.Errorf("failed to resolve context alias %s: %w", context, err)
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
				return filter, fmt.Errorf("failed to resolve actor identifiers %s: %w", actor, err)
			}
			expandedActors = append(expandedActors, allIdentifiers...)
		}
		expandedFilter.Actors = removeDuplicates(expandedActors)
	}

	// Note: We don't expand predicates as they should be resolved via fuzzy matching instead

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
		return nil, fmt.Errorf("failed to resolve AS aliases for %s: %w", identifier, err)
	}
	for _, resolved := range aliasResolved {
		allIdentifiers[resolved] = true
	}

	// 2. Get alternative IDs from EntityResolver (if configured)
	if ae.entityResolver != nil {
		alternativeIDs, err := ae.entityResolver.GetAlternativeIDs(identifier)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve entity alternatives for %s: %w", identifier, err)
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
