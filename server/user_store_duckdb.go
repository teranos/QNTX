//go:build cgo && rustduckdb

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newUserStore returns the User store for the configured backend (ADR-031), or
// nil when the deployment has none.

// Parquet is where a User lives, in the system namespace. On sqlite the result
// is nil and admission records nobody: a login still works, and nothing joins
// the routes that reached it.
func newUserStore(cfg *appcfg.Config) (auth.UserStore, error) {
	if cfg.Storage.Backend != "parquet" {
		//nolint:nilnil // no store is the answer here, not a failure to find one
		return nil, nil
	}

	location := cfg.Storage.Parquet.Location
	store, err := duckdbcgo.NewUserStore(location)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the User store at %s", location)
	}
	return store, nil
}
