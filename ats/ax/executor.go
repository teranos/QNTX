package ax

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// AxExecutor executes ask queries against attestation storage.
//
// The query itself — alias expansion, cartesian claim expansion, conflict
// classification, resolution — runs in `crates/ats`. This type finds the store
// that can reach it and shapes the answer into an AxResult.
type AxExecutor struct {
	queryStore    ats.AttestationQueryStore
	aliasResolver *alias.Resolver
	logger        *zap.SugaredLogger
}

// ResolvedQueryStore is implemented by query stores that can run the whole ax
// read path without returning to Go between steps.
//
// The second return value reports whether that path was available.
type ResolvedQueryStore interface {
	ExecuteAxQueryResolved(ctx context.Context, filter types.AxFilter) ([]*types.As, bool, error)
}

// NewAxExecutor creates a new ask executor.
func NewAxExecutor(queryStore ats.AttestationQueryStore, aliasResolver *alias.Resolver) *AxExecutor {
	return NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{})
}

// AxExecutorOptions provides optional configuration for AxExecutor.
type AxExecutorOptions struct {
	Logger     *zap.SugaredLogger // Optional logger for debug output (default: nil, no logging)
	RawQuerier interface{}        // Optional: routes attestation queries through Rust FFI (storage.RawQuerier)
}

// NewAxExecutorWithOptions creates an executor with custom options.
func NewAxExecutorWithOptions(queryStore ats.AttestationQueryStore, aliasResolver *alias.Resolver, opts AxExecutorOptions) *AxExecutor {
	return &AxExecutor{
		queryStore:    queryStore,
		aliasResolver: aliasResolver,
		logger:        opts.Logger,
	}
}

// ExecuteAsk executes an ask query and returns results.
//
// Requires a store that can reach the Rust read path. Alias resolution and
// classification have no Go implementation — `crates/ats` holds both, and
// `ats::ax` composes them.
func (ae *AxExecutor) ExecuteAsk(ctx context.Context, filter types.AxFilter) (*types.AxResult, error) {
	startTime := time.Now()

	if ae.logger != nil {
		ae.logger.Debugw("executing ax query",
			"subjects", filter.Subjects,
			"predicates", filter.Predicates,
			"contexts", filter.Contexts,
		)
	}

	store, ok := ae.queryStore.(ResolvedQueryStore)
	if !ok {
		return nil, errors.Newf("query store %T cannot execute ax queries: it does not implement ResolvedQueryStore, so it cannot reach alias expansion or classification", ae.queryStore)
	}

	attestationsPtr, supported, err := store.ExecuteAxQueryResolved(ctx, filter)
	if err != nil {
		err = errors.Wrap(err, "failed to execute ax query")
		err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", filter.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", filter.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", filter.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", filter.Actors))
		err = errors.WithDetail(err, fmt.Sprintf("Limit: %d", filter.Limit))
		return nil, err
	}
	if !supported {
		return nil, errors.Newf("query store %T is not wired to the Rust query path: no raw querier is set, so alias expansion and classification are unreachable", ae.queryStore)
	}

	attestations := make([]types.As, len(attestationsPtr))
	for i, as := range attestationsPtr {
		attestations[i] = *as
	}

	result := &types.AxResult{
		Attestations: attestations,
		Conflicts:    []types.Conflict{},
		Summary:      ae.generateSummary(attestations),
		Debug: types.AxDebug{
			OriginalFilter:   filter,
			ExecutionTimeMs:  time.Since(startTime).Milliseconds(),
			DatabaseRowCount: len(attestations),
		},
	}

	if ae.logger != nil {
		ae.logger.Debugw("ax query resolved",
			"attestations", len(attestations),
		)
	}

	return result, nil
}

// generateSummary generates a basic summary of the results.
//
// No caller reads AxResult.Summary — checked across all four consumers of
// ExecuteAsk. It is filled in because the field exists, not because anything
// needs it; whether the shape is right is undecided.
func (ae *AxExecutor) generateSummary(attestations []types.As) types.AxSummary {
	summary := types.AxSummary{
		TotalAttestations: len(attestations),
		UniqueSubjects:    make(map[string]int),
		UniquePredicates:  make(map[string]int),
		UniqueContexts:    make(map[string]int),
		UniqueActors:      make(map[string]int),
	}

	for _, as := range attestations {
		for _, subject := range as.Subjects {
			summary.UniqueSubjects[subject]++
		}

		// The existence placeholder is not a claimed predicate or context.
		for _, predicate := range as.Predicates {
			if predicate != "_" {
				summary.UniquePredicates[predicate]++
			}
		}
		for _, context := range as.Contexts {
			if context != "_" {
				summary.UniqueContexts[context]++
			}
		}

		for _, actor := range as.Actors {
			summary.UniqueActors[actor]++
		}
	}

	return summary
}

// GetAliasResolver returns the alias resolver.
//
// Alias *reads* happen in Rust during the query. This is retained for alias
// writes and inspection, which have no FFI entry point.
func (ae *AxExecutor) GetAliasResolver() *alias.Resolver {
	return ae.aliasResolver
}
