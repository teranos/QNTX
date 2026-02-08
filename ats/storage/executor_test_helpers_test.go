package storage

// Test-only executor helpers. These replicate the assembly logic from ats/setup
// for tests that live inside package storage and can't import setup (circular).

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
)

func newTestExecutor(db *sql.DB) *ax.AxExecutor {
	queryStore := NewSQLQueryStore(db)
	aliasStore := NewAliasStore(db)
	aliasResolver := alias.NewResolver(aliasStore)
	return ax.NewAxExecutor(queryStore, aliasResolver)
}

func newTestExecutorWithOptions(db *sql.DB, opts ax.AxExecutorOptions) *ax.AxExecutor {
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
