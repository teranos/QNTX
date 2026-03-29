//go:build !cgo

package commands

import (
	"database/sql"
	"fmt"

	"github.com/teranos/QNTX/ats"
)

// openDatabase is unavailable without CGO — requires Rust SQLite driver.
func openDatabase(dbPath string) (*sql.DB, ats.AttestationStore, string, error) {
	return nil, nil, "", fmt.Errorf("database unavailable: this binary was built without CGO (use Nix build for full functionality)")
}
