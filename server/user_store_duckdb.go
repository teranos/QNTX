//go:build cgo && rustduckdb

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newUserRecord returns the record behind the users table (ADR-037): on
// parquet, one object per User in the system namespace, read once at open and
// written through after the table. On sqlite there is none, and nil says so.
func newUserRecord(cfg *appcfg.Config) (auth.UserStore, error) {
	if cfg.Storage.Backend != "parquet" {
		//nolint:nilnil // no record is the answer here, not a failure to find one
		return nil, nil
	}

	location := cfg.Storage.Parquet.Location
	store, err := duckdbcgo.NewUserStore(location)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the User record at %s", location)
	}
	return store, nil
}
