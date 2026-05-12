package storage

import (
	"fmt"

	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// SweepStaleEmbeddings deletes embeddings whose source attestations no longer exist.
// Returns the number of stale embeddings removed.
func (s *EmbeddingStore) SweepStaleEmbeddings() (int, error) {
	rows, err := s.db.Query(`
		SELECT e.id, e.model FROM embeddings e
		WHERE e.source_type = 'attestation'
		AND NOT EXISTS (SELECT 1 FROM attestations a WHERE a.id = e.source_id)
	`)
	if err != nil {
		return 0, errors.Wrap(err, "failed to query stale embeddings")
	}
	defer rows.Close()

	type staleEntry struct {
		id    string
		model string
	}
	var stale []staleEntry
	for rows.Next() {
		var e staleEntry
		if err := rows.Scan(&e.id, &e.model); err != nil {
			return 0, errors.Wrap(err, "failed to scan stale embedding")
		}
		stale = append(stale, e)
	}
	if err := rows.Err(); err != nil {
		return 0, errors.Wrap(err, "failed to iterate stale embeddings")
	}

	if len(stale) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin transaction for stale sweep")
	}

	for _, e := range stale {
		table := vecTableName(e.model)
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE embedding_id = ?", table), e.id); err != nil {
			tx.Rollback()
			return 0, errors.Wrapf(err, "failed to delete from %s for stale embedding %s", table, e.id)
		}
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE id = ?`, e.id); err != nil {
			tx.Rollback()
			return 0, errors.Wrapf(err, "failed to delete stale embedding %s", e.id)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit stale embedding sweep")
	}

	s.logger.Info("swept stale embeddings",
		zap.Int("count", len(stale)))

	return len(stale), nil
}
