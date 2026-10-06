// Package storage provides convenience functions for creating ATS executors with storage.
package storage

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
)

// NewExecutor creates an AxExecutor from a database connection and the Rust
// store that shares it.
//
// The raw querier is required, not optional: alias expansion, claim expansion
// and classification live in crates/ats and are reached only through the FFI.
// A `*sql.DB` alone cannot run an ax query.
//
// Example:
//
//	executor := storage.NewExecutor(db, rustStore)
//	result, err := executor.ExecuteAsk(ctx, filter)
func NewExecutor(db *sql.DB, rawQuerier RawQuerier) *ax.AxExecutor {
	queryStore := NewSQLQueryStore(db)
	queryStore.SetRawQuerier(rawQuerier)

	aliasStore := NewAliasStore(db)
	aliasResolver := alias.NewResolver(aliasStore)

	return ax.NewAxExecutor(queryStore, aliasResolver)
}

// NewExecutorWithOptions creates an AxExecutor with custom options.
// Use this when you need a logger, or when the raw querier arrives as an
// interface{} from a plugin's service registry.
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
