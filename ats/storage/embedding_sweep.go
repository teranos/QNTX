package storage

import (
	"github.com/teranos/QNTX/internal/sqlclose"

	"github.com/teranos/QNTX/internal/db"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// SweepStaleEmbeddings deletes embeddings whose source attestations no longer exist.
// Returns the number of stale embeddings removed.
func (s *EmbeddingStore) SweepStaleEmbeddings() (_ int, err error) {
	rows, err := s.db.Query(`
		SELECT e.id, e.model FROM embeddings e
		WHERE e.source_type = 'attestation'
		AND NOT EXISTS (SELECT 1 FROM attestations a WHERE a.id = e.source_id)
	`)
	if err != nil {
		return 0, errors.Wrap(err, "failed to query stale embeddings")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for SweepStaleEmbeddings") }()

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

	// Nothing is stale: what is recorded to be taken out is still asked again.
	if len(stale) == 0 {
		if _, err := s.ForgetPending(); err != nil {
			s.logger.Warn("vectors recorded to be taken out are still indexed", zap.Error(err))
		}
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin transaction for stale sweep")
	}

	recorded := make([]sourceEmbedding, len(stale))
	for i, e := range stale {
		recorded[i] = sourceEmbedding{id: e.id, model: e.model}
	}
	if err := toForget(tx, recorded); err != nil {
		return 0, db.Undone(err, tx)
	}
	for _, e := range stale {
		if _, err := tx.Exec(`DELETE FROM embeddings WHERE id = ?`, e.id); err != nil {
			return 0, db.Undone(errors.Wrapf(err, "failed to delete stale embedding %s", e.id), tx)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit stale embedding sweep")
	}

	s.logger.Info("swept stale embeddings",
		zap.Int("count", len(stale)))

	// The vectors go after the rows; one the index does not take out stays
	// recorded and is asked again (ForgetPending), and is no search result
	// meanwhile, since its row is gone.
	if _, err := s.ForgetPending(); err != nil {
		s.logger.Warn("swept embeddings' vectors are still indexed, and recorded to be taken out", zap.Error(err))
	}

	return len(stale), nil
}
