package storage

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
	vanity "github.com/teranos/vanity-id"
	"go.uber.org/zap"
)

// EmbeddingModel represents a stored embedding in the database
type EmbeddingModel struct {
	ID         string    `json:"id"`
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id"`
	Text       string    `json:"text"`
	Embedding  []byte    `json:"-"` // Binary FLOAT32_BLOB data
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EmbeddingStore provides database operations for embeddings
type EmbeddingStore struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewEmbeddingStore creates a new embedding store
func NewEmbeddingStore(db *sql.DB, logger *zap.Logger) *EmbeddingStore {
	return &EmbeddingStore{
		db:     db,
		logger: logger,
	}
}

// Save stores an embedding in the database
func (s *EmbeddingStore) Save(embedding *EmbeddingModel) error {
	if embedding == nil {
		return errors.New("embedding is nil")
	}

	if embedding.ID == "" {
		// Generate a random 8-character ID for embeddings
		embedding.ID, _ = vanity.GenerateRandomID(8)
	}

	now := time.Now().UTC()
	if embedding.CreatedAt.IsZero() {
		embedding.CreatedAt = now
	}
	embedding.UpdatedAt = now

	query := `
		INSERT INTO embeddings (
			id, source_type, source_id, text, embedding,
			model, dimensions, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			text = excluded.text,
			embedding = excluded.embedding,
			model = excluded.model,
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`

	_, err := s.db.Exec(query,
		embedding.ID,
		embedding.SourceType,
		embedding.SourceID,
		embedding.Text,
		embedding.Embedding,
		embedding.Model,
		embedding.Dimensions,
		embedding.CreatedAt.Format(time.RFC3339),
		embedding.UpdatedAt.Format(time.RFC3339),
	)

	if err != nil {
		return errors.Wrapf(err, "failed to save embedding %s for %s:%s",
			embedding.ID, embedding.SourceType, embedding.SourceID)
	}

	// Also update the vec_embeddings virtual table for vector search
	// Virtual tables don't support UPSERT, so we delete then insert
	_, _ = s.db.Exec("DELETE FROM vec_embeddings WHERE embedding_id = ?", embedding.ID)

	vecQuery := `INSERT INTO vec_embeddings (embedding_id, embedding) VALUES (?, ?)`
	_, err = s.db.Exec(vecQuery, embedding.ID, embedding.Embedding)
	if err != nil {
		return errors.Wrapf(err, "failed to save embedding %s to vec_embeddings table",
			embedding.ID)
	}

	s.logger.Debug("saved embedding",
		zap.String("id", embedding.ID),
		zap.String("source_type", embedding.SourceType),
		zap.String("source_id", embedding.SourceID),
		zap.Int("dimensions", embedding.Dimensions))

	return nil
}

// GetBySource retrieves an embedding by its source
func (s *EmbeddingStore) GetBySource(sourceType, sourceID string) (*EmbeddingModel, error) {
	query := `
		SELECT id, source_type, source_id, text, embedding,
		       model, dimensions, created_at, updated_at
		FROM embeddings
		WHERE source_type = ? AND source_id = ?
	`

	var embedding EmbeddingModel
	var createdAt, updatedAt string

	err := s.db.QueryRow(query, sourceType, sourceID).Scan(
		&embedding.ID,
		&embedding.SourceType,
		&embedding.SourceID,
		&embedding.Text,
		&embedding.Embedding,
		&embedding.Model,
		&embedding.Dimensions,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found is not an error
	}

	if err != nil {
		return nil, errors.Wrapf(err, "failed to get embedding for %s:%s",
			sourceType, sourceID)
	}

	// Parse timestamps
	embedding.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	embedding.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &embedding, nil
}

// SearchResult represents a semantic search result
type SearchResult struct {
	EmbeddingID string  `json:"embedding_id"`
	SourceType  string  `json:"source_type"`
	SourceID    string  `json:"source_id"`
	Text        string  `json:"text"`
	Distance    float32 `json:"distance"`
	Similarity  float32 `json:"similarity"` // 1.0 - normalized distance
}

// SemanticSearch performs a vector similarity search
func (s *EmbeddingStore) SemanticSearch(queryEmbedding []byte, limit int, threshold float32) ([]*SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, errors.New("query embedding is empty")
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Use L2 distance with sqlite-vec
	// Lower distance means more similar
	query := `
		SELECT
			v.embedding_id,
			e.source_type,
			e.source_id,
			e.text,
			vec_distance_L2(v.embedding, ?) as distance
		FROM vec_embeddings v
		JOIN embeddings e ON v.embedding_id = e.id
		ORDER BY distance
		LIMIT ?
	`

	rows, err := s.db.Query(query, queryEmbedding, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to perform semantic search (limit=%d, threshold=%.2f)", limit, threshold)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		var result SearchResult
		err := rows.Scan(
			&result.EmbeddingID,
			&result.SourceType,
			&result.SourceID,
			&result.Text,
			&result.Distance,
		)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to scan search result at row %d", len(results)+1)
		}

		// Convert L2 distance to similarity score
		// Normalize based on typical L2 distances for normalized embeddings
		// L2 distance for normalized vectors ranges from 0 to 2
		// Convert to similarity: 1 - (distance / 2)
		result.Similarity = 1.0 - (result.Distance / 2.0)
		if result.Similarity < 0 {
			result.Similarity = 0
		}

		// Apply threshold filter
		if result.Similarity >= threshold {
			results = append(results, &result)
		}
	}

	if err = rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate search results (scanned %d rows)", len(results))
	}

	s.logger.Debug("semantic search completed",
		zap.Int("results", len(results)),
		zap.Int("limit", limit),
		zap.Float32("threshold", threshold))

	return results, nil
}

// DeleteBySource removes an embedding by its source
func (s *EmbeddingStore) DeleteBySource(sourceType, sourceID string) error {
	// First get the embedding ID for vec_embeddings deletion
	var embeddingID string
	err := s.db.QueryRow(
		`SELECT id FROM embeddings WHERE source_type = ? AND source_id = ?`,
		sourceType, sourceID,
	).Scan(&embeddingID)

	if err == sql.ErrNoRows {
		return nil // Nothing to delete
	}

	if err != nil {
		return errors.Wrapf(err, "failed to find embedding for %s:%s",
			sourceType, sourceID)
	}

	// Delete from vec_embeddings first
	_, err = s.db.Exec(`DELETE FROM vec_embeddings WHERE embedding_id = ?`, embeddingID)
	if err != nil {
		return errors.Wrapf(err, "failed to delete from vec_embeddings for %s",
			embeddingID)
	}

	// Delete from embeddings table
	_, err = s.db.Exec(
		`DELETE FROM embeddings WHERE source_type = ? AND source_id = ?`,
		sourceType, sourceID,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to delete embedding for %s:%s",
			sourceType, sourceID)
	}

	s.logger.Debug("deleted embedding",
		zap.String("id", embeddingID),
		zap.String("source_type", sourceType),
		zap.String("source_id", sourceID))

	return nil
}

// BatchSaveAttestationEmbeddings saves embeddings for multiple attestations
func (s *EmbeddingStore) BatchSaveAttestationEmbeddings(embeddings []*EmbeddingModel) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction for batch save")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				s.logger.Error("failed to rollback batch save transaction",
					zap.Error(rollbackErr))
			}
		}
	}()

	// Prepare statements
	embStmt, err := tx.Prepare(`
		INSERT INTO embeddings (
			id, source_type, source_id, text, embedding,
			model, dimensions, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			text = excluded.text,
			embedding = excluded.embedding,
			model = excluded.model,
			dimensions = excluded.dimensions,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare embeddings insert statement")
	}
	defer embStmt.Close()

	// Virtual tables don't support UPSERT, so we need to delete then insert
	delVecStmt, err := tx.Prepare(`DELETE FROM vec_embeddings WHERE embedding_id = ?`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare vec_embeddings delete statement")
	}
	defer delVecStmt.Close()

	vecStmt, err := tx.Prepare(`INSERT INTO vec_embeddings (embedding_id, embedding) VALUES (?, ?)`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare vec_embeddings insert statement")
	}
	defer vecStmt.Close()

	now := time.Now().UTC()
	for _, embedding := range embeddings {
		if embedding.ID == "" {
			// Generate a random 8-character ID for embeddings
			embedding.ID, _ = vanity.GenerateRandomID(8)
		}
		if embedding.CreatedAt.IsZero() {
			embedding.CreatedAt = now
		}
		embedding.UpdatedAt = now

		// Insert into embeddings table
		_, err = embStmt.Exec(
			embedding.ID,
			embedding.SourceType,
			embedding.SourceID,
			embedding.Text,
			embedding.Embedding,
			embedding.Model,
			embedding.Dimensions,
			embedding.CreatedAt.Format(time.RFC3339),
			embedding.UpdatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return errors.Wrapf(err, "failed to insert embedding %s", embedding.ID)
		}

		// Delete existing vec_embeddings entry if it exists, then insert new one
		_, _ = delVecStmt.Exec(embedding.ID)
		_, err = vecStmt.Exec(embedding.ID, embedding.Embedding)
		if err != nil {
			return errors.Wrapf(err, "failed to insert into vec_embeddings %s", embedding.ID)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit batch save transaction")
	}

	s.logger.Info("batch saved embeddings",
		zap.Int("count", len(embeddings)))

	return nil
}
