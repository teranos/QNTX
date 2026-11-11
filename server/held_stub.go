//go:build !cgo || !rustduckdb || quickdev

package server

import (
	"github.com/teranos/QNTX/server/subsystem"
	errors "github.com/teranos/sacred-error"
)

// openHeld has no DuckDB to open in this build: it links none.
func openHeld(path string, _ []subsystem.Migration) (subsystem.Held, error) {
	return nil, errors.Newf("this build links no DuckDB, so none is held at %s", path)
}
