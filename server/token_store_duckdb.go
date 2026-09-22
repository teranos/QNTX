//go:build cgo && rustduckdb

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newTokenRecord returns the record behind the access_tokens table (ADR-037):
// on parquet, one object per token in the system namespace, read once at open
// and written through after the table.
//
// Parquet is the reference implementation and ships first, so this is the only
// backend wired today. On sqlite the result is nil, which makes the bearer
// path skip and /auth/tokens answer 503 — nothing mints a credential that
// cannot be looked up again.
func newTokenRecord(cfg *appcfg.Config) (auth.TokenRecordStore, bool, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, false, nil
	}

	location := cfg.Storage.Parquet.Location
	store, err := duckdbcgo.NewTokenStore(location)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to open the access token record at %s", location)
	}
	return store, true, nil
}
