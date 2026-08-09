//go:build !cgo || !rustduckdb

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/errors"
)

// newIdentityStore has no backend to open in this build. A parquet deployment
// built this way would keep its node identity in the SQLite scratch and look
// configured, so the error carries the missing tag rather than returning nil.
func newIdentityStore(cfg *appcfg.Config) (nodedid.IdentityStore, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, nil
	}
	return nil, errors.Newf(
		"storage.backend is %q but this binary was built without the rustduckdb tag, "+
			"so the node identity store is not compiled in",
		cfg.Storage.Backend,
	)
}
