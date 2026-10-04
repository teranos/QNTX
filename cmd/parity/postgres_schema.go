//go:build rustpostgres

package main

import (
	"os"

	"github.com/teranos/QNTX/ats/storage/postgrescgo"
	"github.com/teranos/errors"
)

// PostgresSchema returns the tables Postgres ends up with, applied by
// ats-postgres's own runner, and the version the server says it is. The
// server is the one QNTX_POSTGRES_URL names, which make parity starts from the
// pinned Supabase Postgres (scripts/with-postgres.sh). schema_migrations, the
// runner's own bookkeeping, is left out.
func PostgresSchema() (map[string]bool, string, error) {
	url := os.Getenv("QNTX_POSTGRES_URL")
	if url == "" {
		return nil, "", errors.New("QNTX_POSTGRES_URL names no Postgres: make parity starts the pinned one with scripts/with-postgres.sh")
	}
	tables, version, err := postgrescgo.Schema(url, "", "default")
	if err != nil {
		return nil, "", err
	}
	names := map[string]bool{}
	for _, name := range tables {
		if name != "schema_migrations" {
			names[name] = true
		}
	}
	return names, version, nil
}
