// Package storage provides convenience functions for creating ATS executors with storage.
package storage

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
)

// NewExecutor creates a standard AxExecutor with all required dependencies initialized from a database connection.
// This is the recommended way to create an executor for most use cases.
//
// The executor is created with:
//   - SQLQueryStore for attestation queries
//   - AliasStore and Resolver for name normalization
//   - Smart classification enabled (default)
//   - NoOpEntityResolver (no external identity resolution)
//   - NoOpQueryExpander (literal query matching only)
//
// For advanced use cases requiring semantic query expansion or entity deduplication,
// use NewExecutorWithOptions instead.
//
// Example:
//
//	executor := storage.NewExecutor(db)
//	result, err := executor.ExecuteAsk(ctx, filter)
func NewExecutor(db *sql.DB) *ax.AxExecutor {
	queryStore := NewSQLQueryStore(db)
	aliasStore := NewAliasStore(db)
	aliasResolver := alias.NewResolver(aliasStore)
	return ax.NewAxExecutor(queryStore, aliasResolver)
}

// NewExecutorWithOptions creates an AxExecutor with custom QueryExpander and EntityResolver.
// Use this when you need semantic query expansion (e.g., "is engineer" → occupation queries)
// or entity deduplication (resolving entity aliases to canonical IDs).
//
// The executor is created with:
//   - SQLQueryStore (with optional QueryExpander for semantic queries)
//   - AliasStore and Resolver for name normalization
//   - Smart classification enabled (unless opts.UseBasic is true)
//   - Custom EntityResolver (if provided, otherwise NoOpEntityResolver)
//   - Custom QueryExpander (if provided, otherwise NoOpQueryExpander)
//
// Example with semantic query expansion:
//
//	executor := storage.NewExecutorWithOptions(db, ax.AxExecutorOptions{
//	    QueryExpander: &bcs.BCSQueryExpander{},
//	})
//
// Example with both query expansion and entity resolution:
//
//	executor := storage.NewExecutorWithOptions(db, ax.AxExecutorOptions{
//	    QueryExpander:  &bcs.BCSQueryExpander{},
//	    EntityResolver: atsAdapter.NewContactEntityResolver(db),
//	})
func NewExecutorWithOptions(db *sql.DB, opts ax.AxExecutorOptions) *ax.AxExecutor {
	var queryStore *SQLQueryStore
	if opts.QueryExpander != nil {
		queryStore = NewSQLQueryStoreWithExpander(db, opts.QueryExpander)
	} else {
		queryStore = NewSQLQueryStore(db)
	}

	aliasStore := NewAliasStore(db)
	aliasResolver := alias.NewResolver(aliasStore)

	return ax.NewAxExecutorWithOptions(queryStore, aliasResolver, opts)
}
