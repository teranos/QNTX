package server

import (
	"net/http"
	"strconv"

	"github.com/teranos/QNTX/search"
)

// HandleMeiliSearch handles full-text search requests.
// GET /api/search/meilisearch?q=...&limit=20&offset=0&source=...&time_start=...&time_end=...
func (s *QNTXServer) HandleMeiliSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if s.meiliSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "meilisearch not enabled")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	filters := search.SearchFilters{
		Source: r.URL.Query().Get("source"),
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			filters.Limit = v
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
			filters.Offset = v
		}
	}
	if tsStr := r.URL.Query().Get("time_start"); tsStr != "" {
		if v, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			filters.TimeStart = &v
		}
	}
	if tsStr := r.URL.Query().Get("time_end"); tsStr != "" {
		if v, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			filters.TimeEnd = &v
		}
	}

	// Parse array filters from repeated query params
	filters.Subjects = r.URL.Query()["subject"]
	filters.Predicates = r.URL.Query()["predicate"]
	filters.Contexts = r.URL.Query()["context"]
	filters.Actors = r.URL.Query()["actor"]

	resp, err := s.meiliSearch.Search(query, filters)
	if err != nil {
		s.logger.Warnw("Meilisearch search failed", "query", query, "error", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleMeiliReindex triggers a full reindex of all attestations.
// POST /api/search/meilisearch/reindex
func (s *QNTXServer) HandleMeiliReindex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if s.meiliSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "meilisearch not enabled")
		return
	}

	count, err := s.meiliSearch.Reindex(s.atsStore)
	if err != nil {
		s.logger.Errorw("Meilisearch reindex failed", "error", err)
		writeError(w, http.StatusInternalServerError, "reindex failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reindexed": count,
	})
}

// HandleMeiliStats returns Meilisearch index statistics.
// GET /api/search/meilisearch/stats
func (s *QNTXServer) HandleMeiliStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if s.meiliSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "meilisearch not enabled")
		return
	}

	stats, err := s.meiliSearch.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
