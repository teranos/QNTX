//go:build cgo && !rustduckdb

package commands

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
)

// openParquetDatabase stub — this binary was built without -tags rustduckdb,
// so the qntx-duckdb static library and its Go CGO wrapper are not linked in.
// Rebuild with `nix build .#qntx` (which sets the tag) or add rustduckdb to
// your make/go build tags to enable the parquet backend.
func openParquetDatabase(cfg *config.Config) (*sql.DB, ats.AttestationStore, string, any, error) {
	return nil, nil, "", nil, errors.New(
		"parquet backend not available: this binary was built without -tags rustduckdb",
	)
}
