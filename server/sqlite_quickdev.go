//go:build quickdev

package server

import (
	"database/sql"

	"github.com/teranos/QNTX/db"
)

// quickdev is true: this node is the QuickDev distribution, built with
// -tags quickdev, without ATS or WASM.
const quickdev = true

// openSQLite opens another connection to the node's SQLite through the same
// Go sqlite3 library QuickDev opened it with.
func openSQLite(path string) (*sql.DB, error) {
	return db.Open(path, nil)
}
