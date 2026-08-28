//go:build cgo && rustduckdb

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newTokenStore returns the access token store for the configured backend
// (ADR-025), or nil when the deployment has none.
//
// Parquet is the reference implementation and ships first, so this is the
// only backend wired today. On sqlite the result is nil, which makes the
// bearer path skip and /auth/tokens answer 503 — nothing mints a credential
// that cannot be looked up again.
func newTokenStore(cfg *appcfg.Config) (auth.TokenStore, bool, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, false, nil
	}

	location := cfg.Storage.Parquet.Location
	store, err := duckdbcgo.NewTokenStore(location)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to open the access token store at %s", location)
	}
	return store, true, nil
}
