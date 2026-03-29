//go:build !cgo

package db

import (
	"database/sql"

	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

const (
	SQLiteJournalMode   = "WAL"
	SQLiteBusyTimeoutMS = 5000
)

// Open is unavailable without CGO — the SQLite driver requires CGO.
func Open(path string, log *zap.SugaredLogger) (*sql.DB, error) {
	return nil, errors.Newf("database unavailable: CGO required (path: %s)", path)
}

// OpenWithMigrations is unavailable without CGO.
func OpenWithMigrations(path string, logger *zap.SugaredLogger) (*sql.DB, error) {
	return nil, errors.Newf("database unavailable: CGO required (path: %s)", path)
}
