package storage

import (
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// SweepStaleEmbeddings deletes embeddings whose source attestations no longer exist.
// Returns the number of stale embeddings removed.
func (s *EmbeddingStore) SweepStaleEmbeddings() (int, error) {
	rows, err := s.db.Query(`
		SELECT e.id FROM embeddings e
		WHERE e.source_type = 'attestation'
		AND NOT EXISTS (SELECT 1 FROM attestations a WHERE a.id = e.source_id)
	`)
	if err != nil {
		return 0, errors.Wrap(err, "failed to query stale embeddings")
	}
	defer rows.Close()

	var staleIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, errors.Wrap(err, "failed to scan stale embedding ID")
		}
		staleIDs = append(staleIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, errors.Wrap(err, "failed to iterate stale embeddings")
	}

	if len(staleIDs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin transaction for stale sweep")
	}

	for _, id := range staleIDs {
		if _, err := tx.Exec(`DELETE FROM vec_embeddings WHERE embedding_id = ?`, id); err != nil {
			tx.Rollback()
			return 0, errors.Wrapf(err, "failed to delete vec_embedding for stale embedding %s", id)
		}
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE id = ?`, id); err != nil {
			tx.Rollback()
			return 0, errors.Wrapf(err, "failed to delete stale embedding %s", id)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit stale embedding sweep")
	}

	s.logger.Info("swept stale embeddings",
		zap.Int("count", len(staleIDs)))

	return len(staleIDs), nil
}
