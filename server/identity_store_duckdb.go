//go:build cgo && rustduckdb

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/errors"
)

// newIdentityStore returns the node identity store for the configured backend,
// or nil on sqlite — where nodedid.New opens the database store itself.
func newIdentityStore(cfg *appcfg.Config) (nodedid.IdentityStore, bool, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, false, nil
	}

	location := cfg.Storage.Parquet.Location
	store, err := duckdbcgo.NewIdentityStore(location)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to open the node identity store at %s", location)
	}
	return store, true, nil
}
