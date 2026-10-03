//go:build !atsless

package server

import "database/sql"

// atsless is false: this node keeps attestations through ATS.
const atsless = false

// openSQLite opens another connection to the node's SQLite through the Rust
// driver: the same SQLite library instance as the write path. A separate Go
// sqlite3 connection corrupts the WAL, because mattn/go-sqlite3 and rusqlite
// are independent SQLite C libraries with separate WAL-index mappings.
func openSQLite(path string) (*sql.DB, error) {
	return sql.Open("rustsqlite", path)
}
