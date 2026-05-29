package storage

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/teranos/errors"
)

// EnsureFilesystemCanvas guarantees the db has a filesystem-anchored canvas.
// If one already exists, returns its id without modifying anything. Otherwise
// inserts a fresh row named after the basename of dbDir and returns the new id.
// Idempotent.
//
// dbDir is the directory containing the db file — the canvas is anchored to
// that location, like a Jupyter notebook is anchored to its file path.
func EnsureFilesystemCanvas(ctx context.Context, db *sql.DB, dbDir string) (string, error) {
	var existingID string
	err := db.QueryRowContext(ctx,
		"SELECT id FROM canvases WHERE anchor = 'filesystem' ORDER BY created_at LIMIT 1",
	).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if err != sql.ErrNoRows {
		return "", errors.Wrap(err, "query existing filesystem canvas")
	}

	id := uuid.NewString()
	name := filepath.Base(dbDir)

	if _, err := db.ExecContext(ctx,
		"INSERT INTO canvases (id, name, anchor) VALUES (?, ?, 'filesystem')",
		id, name,
	); err != nil {
		return "", errors.Wrapf(err, "insert filesystem canvas (id=%s, name=%s)", id, name)
	}
	return id, nil
}
