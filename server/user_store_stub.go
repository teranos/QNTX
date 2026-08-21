//go:build !cgo || !rustduckdb

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newUserStore has no backend to open in this build.

// The parquet User store lives behind `cgo && rustduckdb` because it
// dynamically links libduckdb (ADR-024). A parquet deployment built without
// that tag would record nobody, so say so rather than look configured.
func newUserStore(cfg *appcfg.Config) (auth.UserStore, error) {
	if cfg.Storage.Backend != "parquet" {
		return nil, nil
	}
	return nil, errors.Newf(
		"storage.backend is %q but this binary was built without the rustduckdb tag, "+
			"so the User store is not compiled in",
		cfg.Storage.Backend,
	)
}
