//go:build cgo && rustduckdb && !quickdev

package server

import (
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	"github.com/teranos/QNTX/server/subsystem"
)

// openHeld opens a subsystem's own DuckDB at path (subsystem.Host.Held).
func openHeld(path string, migrations []subsystem.Migration) (subsystem.Held, error) {
	held := make([]duckdbcgo.HeldMigration, 0, len(migrations))
	for _, m := range migrations {
		held = append(held, duckdbcgo.HeldMigration{Version: m.Version, SQL: m.SQL})
	}
	return duckdbcgo.OpenHeld(path, held)
}
