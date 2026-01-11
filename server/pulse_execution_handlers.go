package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/pulse/schedule"
)

// =======================
// API Response Types
// =======================

// ListExecutionsResponse represents the response for listing job executions
type ListExecutionsResponse struct {
	Executions []schedule.Execution `json:"executions" tstype:"Execution[]"` // From pulse/schedule
	Count      int                  `json:"count"`
	Total      int                  `json:"total"`
	HasMore    bool                 `json:"has_more"`
}

// =======================
// HTTP Handlers
// =======================

// HandleJobExecutions handles requests for execution history
// GET /api/pulse/jobs/{job_id}/executions?limit=50&offset=0&status=completed
// This is called from HandlePulseJob when the path ends with /executions
func (s *QNTXServer) HandleJobExecutions(w http.ResponseWriter, r *http.Request, jobID string) {
	// Only support GET
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Parse query parameters
	limit := parseIntQueryParam(r, "limit", 50, 1, 100)
	offset := parseIntQueryParam(r, "offset", 0, 0, 1000000)
	statusFilter := r.URL.Query().Get("status")

	// Validate status filter if provided
	if statusFilter != "" {
		validStatuses := map[string]bool{
			schedule.ExecutionStatusRunning:   true,
			schedule.ExecutionStatusCompleted: true,
			schedule.ExecutionStatusFailed:    true,
		}
		if !validStatuses[statusFilter] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid status: %s", statusFilter))
			return
		}
	}

	// Get executions from store
	execStore := s.getExecutionStore()
	executions, total, err := execStore.ListExecutions(jobID, limit, offset, statusFilter)
	if err != nil {
		s.logger.Errorw("Failed to list executions", "error", err, "job_id", jobID)
		writeError(w, http.StatusInternalServerError, "Failed to list executions")
		return
	}

	// Convert to response format (flatten pointer slice)
	execResponses := make([]schedule.Execution, 0, len(executions))
	for _, exec := range executions {
		execResponses = append(execResponses, *exec)
	}

	response := ListExecutionsResponse{
		Executions: execResponses,
		Count:      len(executions),
		Total:      total,
		HasMore:    offset+len(executions) < total,
	}

	writeJSON(w, http.StatusOK, response)
}

// HandlePulseExecution handles requests for individual execution
// GET /api/pulse/executions/{execution_id}
// GET /api/pulse/executions/{execution_id}/logs
func (s *QNTXServer) HandlePulseExecution(w http.ResponseWriter, r *http.Request) {
	// Only support GET
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Extract execution ID from path
	parts := extractPathParts(r.URL.Path, "/api/pulse/executions/")
	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid path format")
		return
	}
	executionID := parts[0]

	// Check if requesting logs
	isLogsRequest := len(parts) > 1 && parts[1] == "logs"

	// Get execution from store
	execStore := s.getExecutionStore()
	execution, err := execStore.GetExecution(executionID)
	if err != nil {
		if errors.IsNotFoundError(err) {
			writeError(w, http.StatusNotFound, "Execution not found")
			return
		}
		s.logger.Errorw("Failed to get execution", "error", err, "execution_id", executionID)
		writeError(w, http.StatusInternalServerError, "Failed to get execution")
		return
	}

	// Handle logs request
	if isLogsRequest {
		if execution.Logs == nil || *execution.Logs == "" {
			writeError(w, http.StatusNotFound, "No logs available")
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(*execution.Logs))
		return
	}

	// Handle execution details request
	writeJSON(w, http.StatusOK, execution)
}

// =======================
// Helper Functions
// =======================

// parseIntQueryParam extracts an integer query parameter with validation
func parseIntQueryParam(r *http.Request, name string, defaultValue, min, max int) int {
	valueStr := r.URL.Query().Get(name)
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}

	if value < min {
		return min
	}
	if value > max {
		return max
	}

	return value
}

// Note: writeJSON and writeError functions removed - use writeJSON/writeError from response.go

// getExecutionStore returns the execution store (lazy init helper)
func (s *QNTXServer) getExecutionStore() *schedule.ExecutionStore {
	// TODO: Consider caching the store instance on QNTXServer
	return schedule.NewExecutionStore(s.db)
}
