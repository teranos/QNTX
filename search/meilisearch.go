// Package search provides full-text search over attestations via Meilisearch.
//
// Architecture:
//   - Meilisearch runs as an external process (not embedded)
//   - This package connects via the Meilisearch HTTP API
//   - Real-time indexing via storage.AttestationObserver (zero-lag)
//   - Searchable fields: subjects, predicates, contexts, actors, source, attributes
//   - Filterable fields: subjects, predicates, contexts, actors, source, timestamp
//   - Sortable fields: timestamp, created_at
package search

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

const (
	// IndexName is the Meilisearch index for attestations
	IndexName = "attestations"

	// indexBatchSize is the max documents per batch upsert
	indexBatchSize = 500

	// indexTimeout is how long to wait for Meilisearch task completion
	indexTimeout = 30 * time.Second
)

// Document is the Meilisearch document shape for an attestation.
// Fields are denormalized for search — Meilisearch indexes flat documents.
type Document struct {
	ID             string `json:"id"`
	Subjects       string `json:"subjects"`        // space-joined for FTS
	Predicates     string `json:"predicates"`      // space-joined for FTS
	Contexts       string `json:"contexts"`        // space-joined for FTS
	Actors         string `json:"actors"`          // space-joined for FTS
	Source         string `json:"source"`          // e.g. "cli", "python"
	AttributesText string `json:"attributes_text"` // flattened attribute values for FTS
	Timestamp      int64  `json:"timestamp"`       // unix ms
	CreatedAt      int64  `json:"created_at"`      // unix ms
}

// SearchResult is a single search hit returned to callers.
type SearchResult struct {
	AttestationID string  `json:"attestation_id"`
	Score         float64 `json:"score,omitempty"` // Meilisearch ranking score (when showRankingScore enabled)
}

// SearchResponse wraps search results with metadata.
type SearchResponse struct {
	Query            string         `json:"query"`
	Hits             []SearchResult `json:"hits"`
	TotalHits        int64          `json:"total_hits"`
	ProcessingTimeMS int64          `json:"processing_time_ms"`
}

// SearchFilters constrain a search query.
type SearchFilters struct {
	Subjects   []string // filter to these subjects
	Predicates []string // filter to these predicates
	Contexts   []string // filter to these contexts
	Actors     []string // filter to these actors
	Source     string   // filter to this source
	TimeStart  *int64   // unix ms lower bound
	TimeEnd    *int64   // unix ms upper bound
	Limit      int      // max results (default 20)
	Offset     int      // pagination offset
}

// Service is the Meilisearch search service.
type Service struct {
	client meilisearch.ServiceManager
	index  meilisearch.IndexManager
	logger *zap.SugaredLogger

	// mu guards initialization state
	mu          sync.RWMutex
	initialized bool
}

// New creates a Meilisearch search service.
// Returns an error if the connection to Meilisearch fails.
func New(url, apiKey string, logger *zap.SugaredLogger) (*Service, error) {
	if url == "" {
		return nil, errors.New("meilisearch url cannot be empty")
	}

	client := meilisearch.New(url, meilisearch.WithAPIKey(apiKey))

	// Verify connection
	if !client.IsHealthy() {
		return nil, errors.Newf("meilisearch at %s is not healthy", url)
	}

	s := &Service{
		client: client,
		logger: logger,
	}

	if err := s.ensureIndex(); err != nil {
		return nil, errors.Wrapf(err, "failed to configure meilisearch index at %s", url)
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	logger.Infow("Meilisearch search service initialized", "url", url, "index", IndexName)
	return s, nil
}

// ensureIndex creates the attestations index and configures its settings.
func (s *Service) ensureIndex() error {
	// Create index (idempotent — Meilisearch ignores if exists)
	taskInfo, err := s.client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        IndexName,
		PrimaryKey: "id",
	})
	if err != nil {
		return errors.Wrap(err, "failed to create index")
	}

	// Wait for index creation
	task, err := s.client.WaitForTask(taskInfo.TaskUID, indexTimeout)
	if err != nil {
		return errors.Wrap(err, "timeout waiting for index creation")
	}
	if task.Status == meilisearch.TaskStatusFailed {
		// "index_already_exists" is fine
		if task.Error.Code != "index_already_exists" {
			return errors.Newf("index creation failed: %s", task.Error.Message)
		}
	}

	s.index = s.client.Index(IndexName)

	// Configure searchable attributes (order = ranking priority)
	searchableAttrs := []string{"subjects", "predicates", "contexts", "actors", "source", "attributes_text"}
	if _, err := s.index.UpdateSearchableAttributes(&searchableAttrs); err != nil {
		return errors.Wrap(err, "failed to set searchable attributes")
	}

	// Configure filterable attributes (API requires []interface{})
	filterableAttrs := []interface{}{"subjects", "predicates", "contexts", "actors", "source", "timestamp", "created_at"}
	if _, err := s.index.UpdateFilterableAttributes(&filterableAttrs); err != nil {
		return errors.Wrap(err, "failed to set filterable attributes")
	}

	// Configure sortable attributes
	sortableAttrs := []string{"timestamp", "created_at"}
	if _, err := s.index.UpdateSortableAttributes(&sortableAttrs); err != nil {
		return errors.Wrap(err, "failed to set sortable attributes")
	}

	return nil
}

// Search performs a full-text search over indexed attestations.
func (s *Service) Search(query string, filters SearchFilters) (*SearchResponse, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, errors.New("meilisearch service not initialized")
	}
	s.mu.RUnlock()

	limit := filters.Limit
	if limit <= 0 {
		limit = 20
	}

	searchReq := &meilisearch.SearchRequest{
		Limit:            int64(limit),
		Offset:           int64(filters.Offset),
		ShowRankingScore: true,
		Sort:             []string{"timestamp:desc"},
	}

	// Build filter string
	filterParts := buildFilterParts(filters)
	if len(filterParts) > 0 {
		searchReq.Filter = strings.Join(filterParts, " AND ")
	}

	resp, err := s.index.Search(query, searchReq)
	if err != nil {
		return nil, errors.Wrapf(err, "meilisearch search failed for query %q", query)
	}

	hits := make([]SearchResult, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		result := SearchResult{}
		if raw, ok := hit["id"]; ok {
			var id string
			if json.Unmarshal(raw, &id) == nil {
				result.AttestationID = id
			}
		}
		if raw, ok := hit["_rankingScore"]; ok {
			var score float64
			if json.Unmarshal(raw, &score) == nil {
				result.Score = score
			}
		}
		hits = append(hits, result)
	}

	return &SearchResponse{
		Query:            query,
		Hits:             hits,
		TotalHits:        resp.EstimatedTotalHits,
		ProcessingTimeMS: resp.ProcessingTimeMs,
	}, nil
}

// Index indexes a single attestation into Meilisearch.
func (s *Service) Index(as *types.As) error {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	doc := attestationToDocument(as)
	pk := "id"
	_, err := s.index.AddDocuments([]Document{doc}, &meilisearch.DocumentOptions{PrimaryKey: &pk})
	if err != nil {
		return errors.Wrapf(err, "failed to index attestation %s", as.ID)
	}
	return nil
}

// Reindex re-indexes all attestations from the store.
// Clears the index first, then re-indexes in batches.
func (s *Service) Reindex(store ats.AttestationStore) (int, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return 0, errors.New("meilisearch service not initialized")
	}
	s.mu.RUnlock()

	// Delete all documents first
	taskInfo, err := s.index.DeleteAllDocuments(nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to clear index")
	}
	task, err := s.client.WaitForTask(taskInfo.TaskUID, indexTimeout)
	if err != nil {
		return 0, errors.Wrap(err, "timeout waiting for index clear")
	}
	if task.Status == meilisearch.TaskStatusFailed {
		return 0, errors.Newf("index clear failed: %s", task.Error.Message)
	}

	// Fetch all attestations (no filters, no limit)
	attestations, err := store.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return 0, errors.Wrap(err, "failed to fetch attestations for reindex")
	}

	if len(attestations) == 0 {
		return 0, nil
	}

	// Batch upsert
	total := 0
	for i := 0; i < len(attestations); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(attestations) {
			end = len(attestations)
		}
		batch := attestations[i:end]

		docs := make([]Document, len(batch))
		for j, a := range batch {
			docs[j] = attestationToDocument(a)
		}

		pk := "id"
		_, err := s.index.AddDocuments(docs, &meilisearch.DocumentOptions{PrimaryKey: &pk})
		if err != nil {
			return total, errors.Wrapf(err, "failed to index batch at offset %d", i)
		}
		total += len(docs)
	}

	s.logger.Infow("Reindex complete", "documents", total)
	return total, nil
}

// OnAttestationCreated implements storage.AttestationObserver.
// Called asynchronously by the storage layer after each attestation write.
func (s *Service) OnAttestationCreated(as *types.As) {
	if err := s.Index(as); err != nil {
		s.logger.Warnw("Failed to index attestation in meilisearch",
			"attestation_id", as.ID,
			"error", err,
		)
	}
}

// Healthy returns whether Meilisearch is reachable.
func (s *Service) Healthy() bool {
	return s.client.IsHealthy()
}

// Stats returns index statistics.
func (s *Service) Stats() (map[string]interface{}, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, errors.New("meilisearch service not initialized")
	}
	s.mu.RUnlock()

	stats, err := s.index.GetStats()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get index stats")
	}

	return map[string]interface{}{
		"number_of_documents": stats.NumberOfDocuments,
		"is_indexing":         stats.IsIndexing,
		"field_distribution":  stats.FieldDistribution,
	}, nil
}

// attestationToDocument converts an attestation to a Meilisearch document.
func attestationToDocument(as *types.As) Document {
	doc := Document{
		ID:         as.ID,
		Subjects:   strings.Join(as.Subjects, " "),
		Predicates: strings.Join(as.Predicates, " "),
		Contexts:   strings.Join(as.Contexts, " "),
		Actors:     strings.Join(as.Actors, " "),
		Source:     as.Source,
		Timestamp:  as.Timestamp.UnixMilli(),
		CreatedAt:  as.CreatedAt.UnixMilli(),
	}

	// Flatten attributes to searchable text
	if len(as.Attributes) > 0 {
		doc.AttributesText = flattenAttributes(as.Attributes)
	}

	return doc
}

// flattenAttributes extracts all string values from the attributes map into a single searchable string.
func flattenAttributes(attrs map[string]interface{}) string {
	if len(attrs) == 0 {
		return ""
	}

	var parts []string
	flattenValue(attrs, &parts)
	return strings.Join(parts, " ")
}

// flattenValue recursively extracts string representations from nested values.
func flattenValue(v interface{}, parts *[]string) {
	switch val := v.(type) {
	case string:
		if val != "" {
			*parts = append(*parts, val)
		}
	case map[string]interface{}:
		for _, nested := range val {
			flattenValue(nested, parts)
		}
	case []interface{}:
		for _, item := range val {
			flattenValue(item, parts)
		}
	case json.Number:
		*parts = append(*parts, val.String())
	case float64:
		// JSON numbers decoded as float64
	case bool:
		// Skip booleans — not useful for FTS
	}
}

// buildFilterParts constructs Meilisearch filter expressions from SearchFilters.
func buildFilterParts(f SearchFilters) []string {
	var parts []string

	if len(f.Subjects) > 0 {
		for _, s := range f.Subjects {
			parts = append(parts, "subjects = "+quoteFilter(s))
		}
	}
	if len(f.Predicates) > 0 {
		for _, p := range f.Predicates {
			parts = append(parts, "predicates = "+quoteFilter(p))
		}
	}
	if len(f.Contexts) > 0 {
		for _, c := range f.Contexts {
			parts = append(parts, "contexts = "+quoteFilter(c))
		}
	}
	if len(f.Actors) > 0 {
		for _, a := range f.Actors {
			parts = append(parts, "actors = "+quoteFilter(a))
		}
	}
	if f.Source != "" {
		parts = append(parts, "source = "+quoteFilter(f.Source))
	}
	if f.TimeStart != nil {
		parts = append(parts, "timestamp >= "+formatInt64(*f.TimeStart))
	}
	if f.TimeEnd != nil {
		parts = append(parts, "timestamp <= "+formatInt64(*f.TimeEnd))
	}

	return parts
}

// quoteFilter wraps a value in single quotes for Meilisearch filter syntax.
func quoteFilter(s string) string {
	// Escape single quotes within the value
	escaped := strings.ReplaceAll(s, "'", "\\'")
	return "'" + escaped + "'"
}

// formatInt64 converts an int64 to its string representation for Meilisearch filters.
func formatInt64(n int64) string {
	return strconv.FormatInt(n, 10)
}
