//go:build atsless

package server

import (
	"database/sql"

	"github.com/teranos/QNTX/db"
)

// atsless is true: this node was built with -tags atsless and keeps no
// attestations.
const atsless = true

// openSQLite opens another connection to the node's SQLite through the same
// Go sqlite3 library the ATSless node opened it with.
func openSQLite(path string) (*sql.DB, error) {
	return db.Open(path, nil)
}
