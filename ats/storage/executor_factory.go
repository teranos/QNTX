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

// NewExecutorWithOptions creates an AxExecutor with custom options.
// Use this when you need entity deduplication or Rust FFI query routing.
func NewExecutorWithOptions(db *sql.DB, opts ax.AxExecutorOptions) *ax.AxExecutor {
	queryStore := NewSQLQueryStore(db)

	// Wire raw querier if provided (routes attestation queries through Rust FFI)
	if opts.RawQuerier != nil {
		if rq, ok := opts.RawQuerier.(RawQuerier); ok {
			queryStore.SetRawQuerier(rq)
		}
	}

	aliasStore := NewAliasStore(db)
	aliasResolver := alias.NewResolver(aliasStore)

	return ax.NewAxExecutorWithOptions(queryStore, aliasResolver, opts)
}
