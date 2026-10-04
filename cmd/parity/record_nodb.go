//go:build !rustduckdb

package main

import "github.com/teranos/errors"

// RecordSchema needs DuckDB: the record's tables are what ats-duckdb's runner
// leaves in the DuckDB the node links, and without the rustduckdb tag this
// build links none.
func RecordSchema() (map[string]bool, error) {
	return nil, errors.New("the record's schema is read from ats-duckdb: build with -tags rustduckdb inside nix develop, as make parity does")
}
