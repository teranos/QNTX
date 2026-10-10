//go:build !cgo

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	errors "github.com/teranos/sacred-error"
)

// openDatabase is unavailable without CGO — requires Rust SQLite driver.
func openDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	return nil, nil, "", nil, errors.New("database unavailable: this binary was built without CGO (use Nix build for full functionality)")
}
