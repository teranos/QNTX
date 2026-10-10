//go:build cgo && rustduckdb && rustpostgres && !quickdev

package commands

import (
	"context"

	"github.com/teranos/QNTX/ats/storage/postgrescgo"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/errors"
)

// "You build QNTX's third storage backend on Supabase's free tier, running
// Supabase Postgres 17.11.0.003."
//
// postgresRecord is a namespace's record in Postgres: its own schema at the
// connection string its ns.toml names.
type postgresRecord struct{ *postgrescgo.PostgresStore }

func (postgresRecord) what() string { return "the postgres store" }

// afterSend has nothing to do: a send is rows in a table, and Postgres keeps
// no files to compact.
func (postgresRecord) afterSend(string) error { return nil }

// openPostgresRecord resolves the reference a namespace names and opens its
// schema there, verified against this node's CA unless the connection string
// says sslmode=disable.
func openPostgresRecord(named, ca, namespace string) (record, error) {
	url, err := secretref.Resolve(context.Background(), named)
	if err != nil {
		return nil, errors.Wrapf(err, "the postgres record of %s names %s, which did not resolve", namespace, named)
	}
	store, err := postgrescgo.NewPostgresStore(url, ca, namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "the postgres record of %s did not open", namespace)
	}
	return postgresRecord{store}, nil
}
