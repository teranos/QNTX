package ax

import (
	"context"
	"fmt"
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

// executeAdvancedClassification groups claims, classifies conflicts, and returns
// deterministically ordered results.
//
// All resolution logic (strategy application, sorting) happens in Rust via WASM.
// Go groups claims, calls the classifier, and reconstructs attestations in the
// order Rust returned (confidence desc, recency desc, ID asc).
func (ae *AxExecutor) executeAdvancedClassification(claims []ats.IndividualClaim) ([]types.Conflict, []types.As) {
	// Group claims by key for classification
	claimGroups := make(map[string][]ats.IndividualClaim)

	for _, claim := range claims {
		key := claim.Subject + "|" + claim.Predicate + "|" + claim.Context
		claimGroups[key] = append(claimGroups[key], claim)
	}

	// Perform smart classification (Rust applies resolution strategies and sorts)
	classificationResult := ae.classifier.ClassifyConflicts(claimGroups)

	// Build claim lookup by source ID for ordered reconstruction
	claimsBySourceID := make(map[string]ats.IndividualClaim, len(claims))
	for _, claim := range claims {
		claimsBySourceID[claim.SourceAs.ID] = claim
	}

	// Reconstruct filtered claims in the order Rust returned (pre-sorted)
	var filteredClaims []ats.IndividualClaim
	for _, id := range classificationResult.ResolvedSourceIDs {
		if claim, ok := claimsBySourceID[id]; ok {
			filteredClaims = append(filteredClaims, claim)
		}
	}

	// Convert AdvancedConflicts back to basic Conflicts
	var conflicts []types.Conflict
	for _, advancedConflict := range classificationResult.Conflicts {
		conflicts = append(conflicts, advancedConflict.Conflict)
	}

	// Convert filtered claims back to attestations (preserves sorted order via first-seen dedup)
	attestations := ats.ConvertClaimsToAttestations(filteredClaims)

	return conflicts, attestations
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
