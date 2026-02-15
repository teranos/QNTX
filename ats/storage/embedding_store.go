package storage

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
	vanity "github.com/teranos/vanity-id"
	"go.uber.org/zap"
)

// ClusterNoise is returned by PredictCluster when no cluster is close enough.
// Matches HDBSCAN convention: -1 = noise/outlier.
const ClusterNoise = -1

// EmbeddingModel represents a stored embedding in the database
type EmbeddingModel struct {
	ID                 string    `json:"id"`
	SourceType         string    `json:"source_type"`
	SourceID           string    `json:"source_id"`
	Text               string    `json:"text"`
	Embedding          []byte    `json:"-"` // Binary FLOAT32_BLOB data
	Model              string    `json:"model"`
	Dimensions         int       `json:"dimensions"`
	ClusterID          int       `json:"cluster_id"`
	ClusterProbability float64   `json:"cluster_probability"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
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

// SemanticSearch performs a vector similarity search.
// When clusterID is non-nil, results are scoped to that cluster only.
func (s *EmbeddingStore) SemanticSearch(queryEmbedding []byte, limit int, threshold float32, clusterID *int) ([]*SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, errors.New("query embedding is empty")
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Use L2 distance with sqlite-vec
	// Lower distance means more similar
	var query string
	var args []interface{}

	if clusterID != nil {
		query = `
			SELECT
				v.embedding_id,
				e.source_type,
				e.source_id,
				e.text,
				vec_distance_L2(v.embedding, ?) as distance
			FROM vec_embeddings v
			JOIN embeddings e ON v.embedding_id = e.id
			WHERE e.cluster_id = ?
			ORDER BY distance
			LIMIT ?
		`
		args = []interface{}{queryEmbedding, *clusterID, limit}
	} else {
		query = `
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
		args = []interface{}{queryEmbedding, limit}
	}

	rows, err := s.db.Query(query, args...)
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

// GetAllEmbeddingVectors reads all embedding IDs and blobs for HDBSCAN input.
func (s *EmbeddingStore) GetAllEmbeddingVectors() (ids []string, blobs [][]byte, err error) {
	rows, err := s.db.Query(`SELECT id, embedding FROM embeddings ORDER BY id`)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to query embedding vectors")
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, nil, errors.Wrapf(err, "failed to scan embedding row %d", len(ids)+1)
		}
		ids = append(ids, id)
		blobs = append(blobs, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to iterate embedding rows (read %d)", len(ids))
	}

	return ids, blobs, nil
}

// ClusterAssignment maps an embedding ID to its cluster label and probability.
type ClusterAssignment struct {
	ID          string
	ClusterID   int
	Probability float64
}

// UpdateClusterAssignments batch-updates cluster labels in a single transaction.
func (s *EmbeddingStore) UpdateClusterAssignments(assignments []ClusterAssignment) error {
	if len(assignments) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin cluster assignment transaction")
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				s.logger.Error("failed to rollback cluster assignment", zap.Error(rbErr))
			}
		}
	}()

	stmt, err := tx.Prepare(`UPDATE embeddings SET cluster_id = ?, cluster_probability = ? WHERE id = ?`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare cluster update statement")
	}
	defer stmt.Close()

	for _, a := range assignments {
		if _, err = stmt.Exec(a.ClusterID, a.Probability, a.ID); err != nil {
			return errors.Wrapf(err, "failed to update cluster for embedding %s", a.ID)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit cluster assignments")
	}

	s.logger.Info("updated cluster assignments", zap.Int("count", len(assignments)))
	return nil
}

// ClusterSummary aggregates cluster assignment counts.
type ClusterSummary struct {
	NClusters int         `json:"n_clusters"`
	NNoise    int         `json:"n_noise"`
	NTotal    int         `json:"n_total"`
	Clusters  map[int]int `json:"clusters"` // cluster_id → count
}

// GetClusterSummary returns aggregated cluster counts.
func (s *EmbeddingStore) GetClusterSummary() (*ClusterSummary, error) {
	rows, err := s.db.Query(`SELECT cluster_id, COUNT(*) FROM embeddings GROUP BY cluster_id ORDER BY cluster_id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query cluster summary")
	}
	defer rows.Close()

	summary := &ClusterSummary{
		Clusters: make(map[int]int),
	}

	for rows.Next() {
		var clusterID, count int
		if err := rows.Scan(&clusterID, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan cluster summary row")
		}
		if clusterID < 0 {
			summary.NNoise += count
		} else {
			summary.Clusters[clusterID] = count
			summary.NClusters++
		}
		summary.NTotal += count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate cluster summary rows")
	}

	return summary, nil
}

// ClusterCentroid represents a stored cluster centroid.
type ClusterCentroid struct {
	ClusterID int
	Centroid  []byte // FLOAT32_BLOB (little-endian f32)
	NMembers  int
}

// SaveClusterCentroids replaces all centroids in a single transaction.
func (s *EmbeddingStore) SaveClusterCentroids(centroids []ClusterCentroid) error {
	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin centroid save transaction")
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				s.logger.Error("failed to rollback centroid save", zap.Error(rbErr))
			}
		}
	}()

	if _, err = tx.Exec(`DELETE FROM cluster_centroids`); err != nil {
		return errors.Wrap(err, "failed to clear cluster_centroids")
	}

	stmt, err := tx.Prepare(`INSERT INTO cluster_centroids (cluster_id, centroid, n_members, updated_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare centroid insert")
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range centroids {
		if _, err = stmt.Exec(c.ClusterID, c.Centroid, c.NMembers, now); err != nil {
			return errors.Wrapf(err, "failed to insert centroid for cluster %d", c.ClusterID)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit centroid save")
	}

	s.logger.Info("saved cluster centroids", zap.Int("count", len(centroids)))
	return nil
}

// GetAllClusterCentroids loads all stored centroids for prediction.
func (s *EmbeddingStore) GetAllClusterCentroids() ([]ClusterCentroid, error) {
	rows, err := s.db.Query(`SELECT cluster_id, centroid, n_members FROM cluster_centroids ORDER BY cluster_id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query cluster centroids")
	}
	defer rows.Close()

	var centroids []ClusterCentroid
	for rows.Next() {
		var c ClusterCentroid
		if err := rows.Scan(&c.ClusterID, &c.Centroid, &c.NMembers); err != nil {
			return nil, errors.Wrapf(err, "failed to scan centroid row %d", len(centroids)+1)
		}
		centroids = append(centroids, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate centroid rows (read %d)", len(centroids))
	}

	return centroids, nil
}

// PredictCluster assigns an embedding to the nearest cluster centroid using cosine similarity.
// Returns cluster ID and similarity score, or ClusterNoise if below threshold.
func (s *EmbeddingStore) PredictCluster(
	embedding []float32,
	centroids []ClusterCentroid,
	deserialize func([]byte) ([]float32, error),
	similarity func(a, b []float32) (float32, error),
	threshold float32,
) (clusterID int, prob float64, err error) {
	if len(centroids) == 0 {
		return ClusterNoise, 0, nil
	}

	bestID := ClusterNoise
	var bestSim float32

	for _, c := range centroids {
		centroidVec, err := deserialize(c.Centroid)
		if err != nil {
			return ClusterNoise, 0, errors.Wrapf(err, "failed to deserialize centroid for cluster %d", c.ClusterID)
		}

		sim, err := similarity(embedding, centroidVec)
		if err != nil {
			return ClusterNoise, 0, errors.Wrapf(err, "failed to compute similarity for cluster %d", c.ClusterID)
		}

		if sim > bestSim {
			bestSim = sim
			bestID = c.ClusterID
		}
	}

	if bestSim < threshold {
		return ClusterNoise, 0, nil
	}

	return bestID, float64(bestSim), nil
}
