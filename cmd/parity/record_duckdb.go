//go:build rustduckdb

package main

import "github.com/teranos/QNTX/ats/storage/duckdbcgo"

// RecordSchema returns the tables DuckDB ends up with, applied by ats-duckdb's
// own runner in the DuckDB the node links, minus schema_migrations, the
// runner's own bookkeeping.
func RecordSchema() (map[string]bool, error) {
	tables, err := duckdbcgo.SchemaTables()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, name := range tables {
		if name != "schema_migrations" {
			names[name] = true
		}
	}
	return names, nil
}
