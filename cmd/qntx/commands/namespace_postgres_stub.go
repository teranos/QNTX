//go:build cgo && rustduckdb && !rustpostgres && !quickdev

package commands

import errors "github.com/teranos/sacred-error"

// openPostgresRecord stub: this binary was built without -tags rustpostgres,
// so ats-postgres is not linked in and a postgres namespace cannot open here.
func openPostgresRecord(named, ca, namespace string) (record, error) {
	return nil, errors.Newf("%s keeps its record in postgres, and this binary was built without -tags rustpostgres", namespace)
}
