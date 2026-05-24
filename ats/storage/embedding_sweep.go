package storage

import (
	"fmt"

	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// GetUnembeddedSigmaIDs returns attestation IDs with source='distill' that have no embedding.
func (s *EmbeddingStore) GetUnembeddedSigmaIDs() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT a.id FROM attestations a
		WHERE a.source = 'distill'
		AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.source_type = 'attestation' AND e.source_id = a.id)
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query unembedded sigmas")
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrap(err, "failed to scan unembedded sigma id")
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

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
