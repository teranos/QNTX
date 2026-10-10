//go:build cgo && rustpostgres && !quickdev

package commands

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/postgrescgo"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// openPostgresDatabase is a node whose record is Postgres: "You build QNTX's
// third storage backend on Supabase's free tier, running Supabase Postgres
// 17.11.0.003."
//
// The node is a SQLite node, and its file is the landing file (ADR-037): a
// write lands there first, every read is answered from there, and what it
// holds is sent to Postgres every sendInterval. Opening sends what a process
// that ended left unsent, then takes in what Postgres holds that the file
// lacks — the same order a parquet namespace opens in.
func openPostgresDatabase(cfg *config.Config, dbPath string) (*sql.DB, ats.AttestationStore, string, any, error) {
	url, err := secretref.Resolve(context.Background(), cfg.Storage.Postgres.URL)
	if err != nil {
		return nil, nil, "", nil, errors.Wrap(err, "storage.postgres.url did not resolve")
	}

	database, atsStore, path, handle, err := openSqliteDatabase(dbPath)
	if err != nil {
		return nil, nil, "", nil, err
	}
	landing, ok := handle.(*sqlitecgo.RustStore)
	if !ok {
		err := errors.Newf("the sqlite node at %s handed back %T, not its store", path, handle)
		return nil, nil, "", nil, sqlclose.With(err, database.Close(), "the database")
	}
	fail := func(err error) (*sql.DB, ats.AttestationStore, string, any, error) {
		err = sqlclose.With(err, database.Close(), "the database")
		return nil, nil, "", nil, sqlclose.With(err, landing.Close(), "the rust store")
	}

	record, err := postgrescgo.NewPostgresStore(url, cfg.Storage.Postgres.CA, auth.NamespaceDefault)
	if err != nil {
		return fail(err)
	}
	sent := storage.FileSentMark{Path: path + ".sent"}
	if err := landOnPostgres(landing, record, path, sent); err != nil {
		return fail(sqlclose.With(err, record.Close(), "the postgres record"))
	}
	sacred.Go("postgres.send", func() { sendToPostgres(landing, record, sent) })
	return database, atsStore, path, handle, nil
}

// landOnPostgres sends what the file holds past its send mark, then takes in
// what the record holds from the take-in mark on. A record that does not
// answer is a node that does not open: a file behind its record would answer
// reads with a hole in them.
//
// A file that never sent sends everything it holds. Postgres keeps the first
// of a row it is given twice, so a row the record already has costs nothing,
// and a row written before the file first opened is not left behind.
func landOnPostgres(landing *sqlitecgo.RustStore, record *postgrescgo.PostgresStore, path string, sent storage.FileSentMark) error {
	rows, err := storage.SendOut(landing, record, sent)
	if err != nil {
		return errors.Wrapf(err, "%s could not send to postgres what it held before it closed, %d rows sent", path, rows)
	}
	logger.Logger.Infow("Sent to postgres on open", "file", path, "rows", rows)

	started := time.Now()
	took, err := storage.TakeIn(landing, record, storage.FileMark{Path: path + ".taken-in"})
	if err != nil {
		return errors.Wrapf(err, "%s could not take in what postgres holds", path)
	}
	logger.Logger.Infow("Taken in from postgres",
		"file", path,
		"whole", took.Whole,
		"since", took.Since.UTC().Format(time.RFC3339Nano),
		"found", took.Found,
		"taken_in", took.TakenIn,
		"took", time.Since(started),
	)

	// Everything the file held was sent above, and what was taken in came
	// from the record, so the record has every row the file holds.
	if err := storage.MarkAllSent(landing, sent); err != nil {
		return errors.Wrapf(err, "%s could not mark what postgres holds as sent", path)
	}
	return nil
}

// sendToPostgres sends what the file holds past its send mark every
// sendInterval, for the life of the process.
func sendToPostgres(landing *sqlitecgo.RustStore, record *postgrescgo.PostgresStore, sent storage.FileSentMark) {
	ticker := time.NewTicker(sendInterval)
	defer ticker.Stop()
	for range ticker.C {
		// Per tick: a sender that died would leave the node taking writes it
		// never sends.
		func() {
			defer sacred.Said("postgres.send")
			started := time.Now()
			rows, err := storage.SendOut(landing, record, sent)
			if err != nil {
				logger.Logger.Errorw("Sending to postgres failed; the rows wait in the landing file for the next send",
					"sent", rows, "error", err)
				return
			}
			logger.Logger.Infow("Sent to postgres", "rows", rows, "took", time.Since(started))
		}()
	}
}
