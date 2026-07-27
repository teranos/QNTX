//go:build !cgo || !rustduckdb

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newTokenStore has no backend to open in this build.
//
// The parquet token store lives behind `cgo && rustduckdb` because it
// dynamically links libduckdb (ADR-024). A binary built without that tag can
// still run on sqlite, where there is no token store either — but a parquet
// deployment built this way would silently have no bearer auth, so say so
// rather than return nil and let it look configured.
func newTokenStore(cfg *appcfg.Config) (auth.TokenStore, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, nil
	}
	return nil, errors.Newf(
		"storage.backend is %q but this binary was built without the rustduckdb tag, "+
			"so the access token store is not compiled in",
		cfg.Storage.Backend,
	)
}
