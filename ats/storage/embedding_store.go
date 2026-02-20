package storage

import (
	"database/sql"
	"strings"
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

// GetBySourceIDs retrieves embeddings by a list of source IDs (source_type = 'attestation').
func (s *EmbeddingStore) GetBySourceIDs(sourceIDs []string) ([]EmbeddingModel, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}

	// Build IN clause with placeholders
	placeholders := make([]string, len(sourceIDs))
	args := make([]interface{}, len(sourceIDs))
	for i, id := range sourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT id, source_type, source_id, text, embedding,
		       model, dimensions, created_at, updated_at
		FROM embeddings
		WHERE source_type = 'attestation' AND source_id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query embeddings for %d source IDs", len(sourceIDs))
	}
	defer rows.Close()

	var results []EmbeddingModel
	for rows.Next() {
		var emb EmbeddingModel
		var createdAt, updatedAt string
		if err := rows.Scan(
			&emb.ID, &emb.SourceType, &emb.SourceID, &emb.Text, &emb.Embedding,
			&emb.Model, &emb.Dimensions, &createdAt, &updatedAt,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan embedding row %d", len(results)+1)
		}
		emb.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		emb.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		results = append(results, emb)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate embeddings (read %d)", len(results))
	}

	return results, nil
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

// ProjectionAssignment maps an embedding ID to its 2D projection coordinates for a given method.
type ProjectionAssignment struct {
	ID     string
	Method string
	X      float64
	Y      float64
}

// UpdateProjections batch-upserts projection coordinates for a given method.
func (s *EmbeddingStore) UpdateProjections(method string, assignments []ProjectionAssignment) error {
	if len(assignments) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin projection update transaction")
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				s.logger.Error("failed to rollback projection update", zap.Error(rbErr))
			}
		}
	}()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO embedding_projections (embedding_id, method, x, y, created_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare projection upsert statement")
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, a := range assignments {
		if _, err = stmt.Exec(a.ID, method, a.X, a.Y, now); err != nil {
			return errors.Wrapf(err, "failed to upsert projection for embedding %s method %s", a.ID, method)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit projection updates")
	}

	s.logger.Info("updated projections", zap.String("method", method), zap.Int("count", len(assignments)))
	return nil
}

// ProjectionWithCluster holds a 2D projection along with its cluster assignment.
type ProjectionWithCluster struct {
	ID        string  `json:"id"`
	SourceID  string  `json:"source_id"`
	Method    string  `json:"method"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	ClusterID int     `json:"cluster_id"`
}

// GetProjectionsByMethod returns all projections for a given method, joined with cluster info.
func (s *EmbeddingStore) GetProjectionsByMethod(method string) ([]ProjectionWithCluster, error) {
	rows, err := s.db.Query(`
		SELECT ep.embedding_id, e.source_id, ep.method, ep.x, ep.y, e.cluster_id
		FROM embedding_projections ep
		JOIN embeddings e ON ep.embedding_id = e.id
		WHERE ep.method = ?
		ORDER BY ep.embedding_id
	`, method)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query projections for method %s", method)
	}
	defer rows.Close()

	var results []ProjectionWithCluster
	for rows.Next() {
		var p ProjectionWithCluster
		if err := rows.Scan(&p.ID, &p.SourceID, &p.Method, &p.X, &p.Y, &p.ClusterID); err != nil {
			return nil, errors.Wrapf(err, "failed to scan projection row %d for method %s", len(results)+1, method)
		}
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate projection rows for method %s (read %d)", method, len(results))
	}

	return results, nil
}

// GetAllProjectionMethods returns distinct method names that have stored projections.
func (s *EmbeddingStore) GetAllProjectionMethods() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT method FROM embedding_projections ORDER BY method`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query projection methods")
	}
	defer rows.Close()

	var methods []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, errors.Wrap(err, "failed to scan projection method")
		}
		methods = append(methods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate projection methods")
	}

	return methods, nil
}

// ClusterRun records metadata about a single HDBSCAN clustering run.
type ClusterRun struct {
	ID             string
	NPoints        int
	NClusters      int
	NNoise         int
	MinClusterSize int
	DurationMS     int
	CreatedAt      time.Time
}

// ClusterIdentity tracks a stable cluster across runs.
type ClusterIdentity struct {
	ID             int
	Label          *string
	FirstSeenRunID string
	LastSeenRunID  string
	Status         string
	CreatedAt      time.Time
}

// ClusterSnapshot records a cluster's centroid at a specific run.
type ClusterSnapshot struct {
	ClusterID int
	RunID     string
	Centroid  []byte
	NMembers  int
	CreatedAt time.Time
}

// ClusterEvent records birth/death/stable transitions.
type ClusterEvent struct {
	RunID      string
	EventType  string // "birth", "death", "stable"
	ClusterID  int
	Similarity *float64
}

// CreateClusterRun inserts a clustering run record.
func (s *EmbeddingStore) CreateClusterRun(run *ClusterRun) error {
	_, err := s.db.Exec(
		`INSERT INTO cluster_runs (id, n_points, n_clusters, n_noise, min_cluster_size, duration_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.NPoints, run.NClusters, run.NNoise, run.MinClusterSize, run.DurationMS,
		run.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to insert cluster run %s", run.ID)
	}
	return nil
}

// UpdateClusterRunDuration sets the final duration_ms on a cluster run.
func (s *EmbeddingStore) UpdateClusterRunDuration(runID string, durationMS int) error {
	_, err := s.db.Exec(`UPDATE cluster_runs SET duration_ms = ? WHERE id = ?`, durationMS, runID)
	if err != nil {
		return errors.Wrapf(err, "failed to update duration for cluster run %s", runID)
	}
	return nil
}

// CreateCluster inserts a new cluster identity and returns the allocated ID.
// Uses SQLite's INTEGER PRIMARY KEY auto-assignment (atomic, no race condition).
func (s *EmbeddingStore) CreateCluster(runID string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.Exec(
		`INSERT INTO clusters (label, first_seen_run_id, last_seen_run_id, status, created_at) VALUES (NULL, ?, ?, 'active', ?)`,
		runID, runID, now,
	)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to insert cluster for run %s", runID)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get allocated cluster ID for run %s", runID)
	}
	return int(id), nil
}

// UpdateClusterLastSeen bumps the last_seen_run_id for a cluster.
func (s *EmbeddingStore) UpdateClusterLastSeen(clusterID int, runID string) error {
	_, err := s.db.Exec(
		`UPDATE clusters SET last_seen_run_id = ? WHERE id = ?`,
		runID, clusterID,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to update last_seen for cluster %d", clusterID)
	}
	return nil
}

// DissolveCluster marks a cluster as dissolved and records the run.
func (s *EmbeddingStore) DissolveCluster(clusterID int, runID string) error {
	_, err := s.db.Exec(
		`UPDATE clusters SET status = 'dissolved', last_seen_run_id = ? WHERE id = ?`,
		runID, clusterID,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to dissolve cluster %d in run %s", clusterID, runID)
	}
	return nil
}

// SaveClusterSnapshots batch-inserts snapshots for a run.
func (s *EmbeddingStore) SaveClusterSnapshots(snapshots []ClusterSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin snapshot transaction")
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				s.logger.Error("failed to rollback snapshot save", zap.Error(rbErr))
			}
		}
	}()

	stmt, err := tx.Prepare(`INSERT INTO cluster_snapshots (cluster_id, run_id, centroid, n_members, created_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare snapshot insert")
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, snap := range snapshots {
		if _, err = stmt.Exec(snap.ClusterID, snap.RunID, snap.Centroid, snap.NMembers, now); err != nil {
			return errors.Wrapf(err, "failed to insert snapshot for cluster %d run %s", snap.ClusterID, snap.RunID)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit snapshot save")
	}
	return nil
}

// RecordClusterEvents batch-inserts cluster events for a run.
func (s *EmbeddingStore) RecordClusterEvents(events []ClusterEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrap(err, "failed to begin event transaction")
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				s.logger.Error("failed to rollback event save", zap.Error(rbErr))
			}
		}
	}()

	stmt, err := tx.Prepare(`INSERT INTO cluster_events (run_id, event_type, cluster_id, similarity, created_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare event insert")
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ev := range events {
		if _, err = stmt.Exec(ev.RunID, ev.EventType, ev.ClusterID, ev.Similarity, now); err != nil {
			return errors.Wrapf(err, "failed to insert %s event for cluster %d", ev.EventType, ev.ClusterID)
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit event save")
	}
	return nil
}

// GetActiveClusterIdentities returns all clusters with status = 'active'.
func (s *EmbeddingStore) GetActiveClusterIdentities() ([]ClusterIdentity, error) {
	rows, err := s.db.Query(`SELECT id, label, first_seen_run_id, last_seen_run_id, status, created_at FROM clusters WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query active cluster identities")
	}
	defer rows.Close()

	var result []ClusterIdentity
	for rows.Next() {
		var c ClusterIdentity
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Label, &c.FirstSeenRunID, &c.LastSeenRunID, &c.Status, &createdAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan cluster identity at row %d", len(result)+1)
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate cluster identities (read %d)", len(result))
	}
	return result, nil
}

// ClusterDetail holds a cluster identity with resolved timestamps and member count.
type ClusterDetail struct {
	ID        int
	Label     *string
	Members   int
	Status    string
	FirstSeen time.Time
	LastSeen  time.Time
}

// GetClusterDetails returns active clusters with member counts and resolved run timestamps.
func (s *EmbeddingStore) GetClusterDetails() ([]ClusterDetail, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.label, c.status,
		       COALESCE(r1.created_at, c.created_at) AS first_seen,
		       COALESCE(r2.created_at, c.created_at) AS last_seen,
		       COALESCE(m.cnt, 0) AS members
		FROM clusters c
		LEFT JOIN cluster_runs r1 ON r1.id = c.first_seen_run_id
		LEFT JOIN cluster_runs r2 ON r2.id = c.last_seen_run_id
		LEFT JOIN (SELECT cluster_id, COUNT(*) AS cnt FROM embeddings WHERE cluster_id >= 0 GROUP BY cluster_id) m ON m.cluster_id = c.id
		WHERE c.status = 'active'
		ORDER BY c.id
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query cluster details")
	}
	defer rows.Close()

	var result []ClusterDetail
	for rows.Next() {
		var d ClusterDetail
		var firstSeen, lastSeen string
		if err := rows.Scan(&d.ID, &d.Label, &d.Status, &firstSeen, &lastSeen, &d.Members); err != nil {
			return nil, errors.Wrapf(err, "failed to scan cluster detail at row %d", len(result)+1)
		}
		d.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
		d.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate cluster details (read %d)", len(result))
	}
	return result, nil
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

// LabelEligibleCluster holds cluster info for the labeling job.
type LabelEligibleCluster struct {
	ID      int
	Members int
}

// GetLabelEligibleClusters returns active clusters eligible for labeling:
// member count >= minSize and (never labeled or labeled_at older than cooldownDays).
// Ordered by member count descending (label biggest first), limited to `limit`.
func (s *EmbeddingStore) GetLabelEligibleClusters(minSize, cooldownDays, limit int) ([]LabelEligibleCluster, error) {
	rows, err := s.db.Query(`
		SELECT c.id, COALESCE(m.cnt, 0) AS members
		FROM clusters c
		LEFT JOIN (SELECT cluster_id, COUNT(*) AS cnt FROM embeddings WHERE cluster_id >= 0 GROUP BY cluster_id) m ON m.cluster_id = c.id
		WHERE c.status = 'active'
		  AND COALESCE(m.cnt, 0) >= ?
		  AND (c.labeled_at IS NULL OR datetime(c.labeled_at) < datetime('now', printf('-%d days', ?)))
		ORDER BY members DESC
		LIMIT ?
	`, minSize, cooldownDays, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query label-eligible clusters (minSize=%d, cooldown=%dd)", minSize, cooldownDays)
	}
	defer rows.Close()

	var result []LabelEligibleCluster
	for rows.Next() {
		var c LabelEligibleCluster
		if err := rows.Scan(&c.ID, &c.Members); err != nil {
			return nil, errors.Wrapf(err, "failed to scan eligible cluster at row %d", len(result)+1)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate eligible clusters (read %d)", len(result))
	}
	return result, nil
}

// SampleClusterTexts returns random member texts from a cluster.
func (s *EmbeddingStore) SampleClusterTexts(clusterID, sampleSize int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT text FROM embeddings WHERE cluster_id = ? ORDER BY RANDOM() LIMIT ?`,
		clusterID, sampleSize,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sample texts for cluster %d", clusterID)
	}
	defer rows.Close()

	var texts []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, errors.Wrapf(err, "failed to scan sample text for cluster %d at row %d", clusterID, len(texts)+1)
		}
		texts = append(texts, text)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate sample texts for cluster %d (read %d)", clusterID, len(texts))
	}
	return texts, nil
}

// ClusterTimelinePoint represents one cluster's state at one run.
type ClusterTimelinePoint struct {
	RunID     string  `json:"run_id"`
	RunTime   string  `json:"run_time"`
	NPoints   int     `json:"n_points"`
	NNoise    int     `json:"n_noise"`
	ClusterID int     `json:"cluster_id"`
	Label     *string `json:"label"`
	NMembers  int     `json:"n_members"`
	EventType string  `json:"event_type"`
}

// GetClusterTimeline returns per-cluster member counts across all runs,
// ordered by run time ASC then cluster ID ASC.
func (s *EmbeddingStore) GetClusterTimeline() ([]ClusterTimelinePoint, error) {
	rows, err := s.db.Query(`
		SELECT cr.id, cr.created_at, cr.n_points, cr.n_noise,
		       cs.cluster_id, c.label, cs.n_members, ce.event_type
		FROM cluster_runs cr
		JOIN cluster_snapshots cs ON cs.run_id = cr.id
		JOIN clusters c ON c.id = cs.cluster_id
		LEFT JOIN (
			SELECT run_id, cluster_id, MIN(event_type) AS event_type
			FROM cluster_events
			GROUP BY run_id, cluster_id
		) ce ON ce.run_id = cr.id AND ce.cluster_id = cs.cluster_id
		ORDER BY cr.created_at ASC, cs.cluster_id ASC
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query cluster timeline")
	}
	defer rows.Close()

	var result []ClusterTimelinePoint
	for rows.Next() {
		var p ClusterTimelinePoint
		var eventType *string
		if err := rows.Scan(&p.RunID, &p.RunTime, &p.NPoints, &p.NNoise,
			&p.ClusterID, &p.Label, &p.NMembers, &eventType); err != nil {
			return nil, errors.Wrapf(err, "failed to scan timeline row %d", len(result)+1)
		}
		if eventType != nil {
			p.EventType = *eventType
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate timeline rows (read %d)", len(result))
	}
	return result, nil
}

// UpdateClusterLabel sets the label and labeled_at timestamp for a cluster.
func (s *EmbeddingStore) UpdateClusterLabel(clusterID int, label string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE clusters SET label = ?, labeled_at = ? WHERE id = ?`,
		label, now, clusterID,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to update label for cluster %d", clusterID)
	}
	return nil
}
