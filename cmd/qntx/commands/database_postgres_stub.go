//go:build cgo && (!rustpostgres || quickdev)

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
)

// openPostgresDatabase stub — this binary was built without -tags
// rustpostgres, so ats-postgres and its Go CGO wrapper are not linked in.
func openPostgresDatabase(cfg *config.Config, dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	return nil, nil, "", nil, errors.New(
		"postgres backend not available: this binary was built without -tags rustpostgres",
	)
}
