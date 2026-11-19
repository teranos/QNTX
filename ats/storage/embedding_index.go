package storage

import (
	"database/sql"
	"encoding/binary"
	"math"
	"strings"
	"sync"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
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

// vectorExec is what a vector to forget is recorded through: the store's
// database, or a transaction the rows are deleted in.
type vectorExec interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// toForget records embeddings whose vectors are to be taken out of their
// models' indexes, before their rows are deleted, so a vector the index did
// not take out is asked again rather than lost with the row.
func toForget(db vectorExec, embeddings []sourceEmbedding) error {
	for _, e := range embeddings {
		if _, err := db.Exec(`INSERT OR IGNORE INTO embedding_vectors_to_forget (embedding_id, model) VALUES (?, ?)`, e.id, e.model); err != nil {
			return errors.Wrapf(err, "the vector of %s of %s was not recorded to be taken out", e.id, e.model)
		}
	}
	return nil
}

// ForgetPending takes every recorded vector out of its model's index, and
// forgets the record of each the index took out. A model whose vectors the
// index did not take out stays recorded, to be asked again.
func (s *EmbeddingStore) ForgetPending() (forgotten int, err error) {
	byModel, err := s.pendingForgets()
	if err != nil {
		return 0, err
	}
	if len(byModel) == 0 {
		return 0, nil
	}
	index, err := s.index()
	if err != nil {
		return 0, NotIndexed{why: err}
	}
	var first error
	for model, ids := range byModel {
		if err := index.Forget(model, ids); err != nil {
			if first == nil {
				first = NotIndexed{why: errors.Wrapf(err, "%d vectors of %s", len(ids), model)}
			}
			continue
		}
		for _, id := range ids {
			if _, err := s.db.Exec(`DELETE FROM embedding_vectors_to_forget WHERE embedding_id = ? AND model = ?`, id, model); err != nil {
				return forgotten, errors.Wrapf(err, "the record of %s of %s, taken out of its index, was not forgotten", id, model)
			}
			forgotten++
		}
	}
	return forgotten, first
}

// pendingForgets is every recorded vector to take out, by model.
func (s *EmbeddingStore) pendingForgets() (_ map[string][]string, err error) {
	rows, err := s.db.Query(`SELECT embedding_id, model FROM embedding_vectors_to_forget`)
	if err != nil {
		return nil, errors.Wrap(err, "the vectors recorded to be taken out were not read")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for pendingForgets") }()
	byModel := map[string][]string{}
	for rows.Next() {
		var id, model string
		if err := rows.Scan(&id, &model); err != nil {
			return nil, errors.Wrap(err, "a vector recorded to be taken out did not read")
		}
		byModel[model] = append(byModel[model], id)
	}
	return byModel, rows.Err()
}

// IndexKept offers every kept embedding to its model's index, and takes out
// what is recorded to be taken out. It is asked when a vector search provider
// registers: what was kept while none was, or before the store had an index,
// is then searchable. Adding a vector the index holds already replaces it.
func (s *EmbeddingStore) IndexKept() (kept int, err error) {
	if _, err := s.ForgetPending(); err != nil {
		s.logger.Warn("vectors recorded to be taken out are still indexed", zap.Error(err))
	}
	index, err := s.index()
	if err != nil {
		return 0, err
	}
	models, err := s.CountEmbeddingsByModel()
	if err != nil {
		return 0, err
	}
	var first error
	for _, model := range models {
		ids, blobs, err := s.GetAllEmbeddingVectors(model.Model)
		if err != nil {
			return kept, err
		}
		for i, id := range ids {
			vector, err := floatsOf(blobs[i])
			if err == nil {
				err = index.Keep(model.Model, model.Dimensions, id, vector)
			}
			if err != nil {
				if first == nil {
					first = NotIndexed{why: errors.Wrapf(err, "embedding %s of %s", id, model.Model)}
				}
				continue
			}
			kept++
		}
	}
	return kept, first
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
	// A hit whose row was swept, or of another cluster, is no result, and the
	// cluster is the store's to know: the index is asked for more until the
	// search holds limit results or the index has no more to answer.
	for asked := limit; ; asked *= 4 {
		hits, err := index.Nearest(model, query, asked)
		if err != nil {
			return nil, errors.Wrapf(err, "the nearest %d to the query in %q", asked, model)
		}
		results, beyond, err := s.resultsOf(hits, limit, threshold, clusterID)
		if err != nil {
			return nil, err
		}
		// Nearest first: past the threshold, no further hit is similar enough.
		if len(results) == limit || len(hits) < asked || beyond {
			return results, nil
		}
	}
}

// resultsOf is the hits that are results, nearest first, up to limit, and
// whether a hit fell below the threshold, past which none further is a result.
func (s *EmbeddingStore) resultsOf(hits []VectorHit, limit int, threshold float32, clusterID *int) ([]*SearchResult, bool, error) {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ID
	}
	rows, err := s.rowsByID(ids)
	if err != nil {
		return nil, false, err
	}
	var results []*SearchResult
	for _, hit := range hits {
		similarity := 1 - hit.Distance
		if similarity < threshold {
			return results, true, nil
		}
		row, held := rows[hit.ID]
		if !held {
			continue
		}
		if clusterID != nil && row.clusterID != *clusterID {
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
	return results, false, nil
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
