//go:build !rustduckdb

package main

import errors "github.com/teranos/sacred-error"

// DuckDBSchema needs DuckDB: the tables are what ats-duckdb's runner leaves in
// the DuckDB the node links, and without the rustduckdb tag this build links
// none.
func DuckDBSchema() (map[string]bool, error) {
	return nil, errors.New("the duckdb schema is read from ats-duckdb: build with -tags rustduckdb inside nix develop, as make parity does")
}
