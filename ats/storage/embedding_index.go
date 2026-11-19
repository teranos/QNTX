package storage

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
)

// VectorIndex is where the store's vectors are kept and found: vector search,
// a service a plugin provides (ADR-016), an index per model (ADR-019). The
// store keeps the rows: the text, the source, the model, the cluster.
type VectorIndex interface {
	// Keep puts one vector into the model's index under the embedding's id.
	Keep(model string, dimensions int, id string, vector []float32) error
	// Forget takes vectors out of the model's index by the embeddings' ids.
	Forget(model string, ids []string) error
	// Nearest is the vectors of the model's index nearest to query, nearest
	// first. An empty model is the embedding provider's first.
	Nearest(model string, query []float32, limit int) ([]VectorHit, error)
}

// VectorHit is one vector an index found: the embedding's id and its distance.
type VectorHit struct {
	ID       string
	Distance float32
}

// NotIndexed is an embedding whose row is kept and whose vector is not: the
// index did not take it, and why.
type NotIndexed struct{ why error }

func (n NotIndexed) Error() string {
	return "the embedding is kept and its vector is not indexed: " + n.why.Error()
}

func (n NotIndexed) Unwrap() error { return n.why }

// indexHeld is the store's vector index, handed by the node once it runs one.
type indexHeld struct {
	mu    sync.RWMutex
	index VectorIndex
}

// SetIndex hands the store where its vectors are kept and found.
func (s *EmbeddingStore) SetIndex(index VectorIndex) {
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	s.vectors.index = index
}

// index is the store's vector index, or why it has none.
func (s *EmbeddingStore) index() (VectorIndex, error) {
	s.vectors.mu.RLock()
	defer s.vectors.mu.RUnlock()
	if s.vectors.index == nil {
		return nil, errors.New("no vector index was handed to the embedding store: vector search is a service a plugin provides")
	}
	return s.vectors.index, nil
}

// floatsOf is a FLOAT32_BLOB as its numbers: little-endian float32s.
func floatsOf(blob []byte) ([]float32, error) {
	if len(blob) == 0 || len(blob)%4 != 0 {
		return nil, errors.Newf("an embedding of %d bytes is no list of float32s", len(blob))
	}
	floats := make([]float32, len(blob)/4)
	for i := range floats {
		floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return floats, nil
}

// keep puts a saved embedding's vector into its model's index.
func (s *EmbeddingStore) keep(embedding *EmbeddingModel) error {
	index, err := s.index()
	if err != nil {
		return NotIndexed{why: err}
	}
	vector, err := floatsOf(embedding.Embedding)
	if err != nil {
		return NotIndexed{why: errors.Wrapf(err, "embedding %s", embedding.ID)}
	}
	if err := index.Keep(embedding.Model, embedding.Dimensions, embedding.ID, vector); err != nil {
		return NotIndexed{why: errors.Wrapf(err, "embedding %s of %s", embedding.ID, embedding.Model)}
	}
	return nil
}

// forget takes deleted embeddings' vectors out of their models' indexes, by
// model.
func (s *EmbeddingStore) forget(byModel map[string][]string) error {
	if len(byModel) == 0 {
		return nil
	}
	index, err := s.index()
	if err != nil {
		return NotIndexed{why: err}
	}
	for model, ids := range byModel {
		if err := index.Forget(model, ids); err != nil {
			return NotIndexed{why: errors.Wrapf(err, "%d vectors of %s", len(ids), model)}
		}
	}
	return nil
}

// SemanticSearch is the embeddings nearest to a query embedding, through the
// vector index: nearest first, as similar as threshold or more, of a cluster
// when clusterID names one. An empty model is the embedding provider's first.
// Similarity is 1 - distance, the cosine similarity for the cosine distance
// the index answers.
func (s *EmbeddingStore) SemanticSearch(queryEmbedding []byte, limit int, threshold float32, clusterID *int, model string) ([]*SearchResult, error) {
	query, err := floatsOf(queryEmbedding)
	if err != nil {
		return nil, errors.Wrap(err, "the query embedding")
	}
	if limit <= 0 {
		limit = 10
	}
	index, err := s.index()
	if err != nil {
		return nil, err
	}
	// The cluster is the store's to know, so a search within one asks the
	// index for more than it answers.
	asked := limit
	if clusterID != nil {
		asked = min(limit*10, 1000)
	}
	hits, err := index.Nearest(model, query, asked)
	if err != nil {
		return nil, errors.Wrapf(err, "the nearest %d to the query in %q", asked, model)
	}
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ID
	}
	rows, err := s.rowsByID(ids)
	if err != nil {
		return nil, err
	}

	var results []*SearchResult
	for _, hit := range hits {
		row, held := rows[hit.ID]
		// A vector whose row was swept is no result.
		if !held {
			continue
		}
		if clusterID != nil && row.clusterID != *clusterID {
			continue
		}
		similarity := 1 - hit.Distance
		if similarity < threshold {
			continue
		}
		results = append(results, &SearchResult{
			EmbeddingID: hit.ID, SourceType: row.sourceType, SourceID: row.sourceID, Text: row.text,
			Distance: hit.Distance, Similarity: similarity,
		})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

// searchedRow is what a search answers of an embedding's row.
type searchedRow struct {
	sourceType, sourceID, text string
	clusterID                  int
}

// rowsByID is the rows of these embeddings, by id.
func (s *EmbeddingStore) rowsByID(ids []string) (_ map[string]searchedRow, err error) {
	held := map[string]searchedRow{}
	if len(ids) == 0 {
		return held, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT id, source_type, source_id, text, COALESCE(cluster_id, -1)
		FROM embeddings WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %d embeddings a search found", len(ids))
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for rowsByID") }()
	for rows.Next() {
		var id string
		var row searchedRow
		if err := rows.Scan(&id, &row.sourceType, &row.sourceID, &row.text, &row.clusterID); err != nil {
			return nil, errors.Wrapf(err, "failed to scan an embedding a search found, after %d", len(held))
		}
		held[id] = row
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to read the embeddings a search found (read %d)", len(held))
	}
	return held, nil
}
