//go:build !cgo || !rustduckdb

package server

import (
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// newUserRecord has no record to open in this build.

// The parquet User record lives behind `cgo && rustduckdb` because it
// dynamically links libduckdb (ADR-024). A parquet deployment built without
// that tag would keep Users in the table alone and lose them with the host,
// so say so rather than look configured.
func newUserRecord(cfg *appcfg.Config) (auth.UserStore, error) {
	if cfg.Storage.Backend != "parquet" {
		//nolint:nilnil // no record is the answer here, not a failure to find one
		return nil, nil
	}
	return nil, errors.Newf(
		"storage.backend is %q but this binary was built without the rustduckdb tag, "+
			"so the User record is not compiled in",
		cfg.Storage.Backend,
	)
}
