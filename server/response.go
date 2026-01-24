package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		return errors.Wrap(err, "failed to encode JSON")
	}
	return nil
}

// writeError writes a JSON error response
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeRichError writes a rich error response with details.
// It logs the full error internally and returns a structured response to clients.
// Use this for API errors where you want to provide context without exposing internals.
func writeRichError(w http.ResponseWriter, logger *zap.SugaredLogger, err error, statusCode int) {
	// Log full error with stack trace internally
	if logger != nil {
		logger.Errorw("Request failed",
			"status", statusCode,
			"error", err,
			"details", errors.FlattenDetails(err),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Return structured error response
	errorResponse := map[string]interface{}{
		"error":   err.Error(),
		"details": errors.FlattenDetails(err),
	}

	if encErr := json.NewEncoder(w).Encode(errorResponse); encErr != nil && logger != nil {
		logger.Errorw("Failed to encode error response", "error", encErr)
	}
}

// writeWrappedError wraps an error with context, logs it, and writes an appropriate response.
// This is the preferred method for handling errors in HTTP handlers.
func writeWrappedError(w http.ResponseWriter, logger *zap.SugaredLogger, err error, context string, statusCode int) {
	wrappedErr := errors.Wrap(err, context)
	writeRichError(w, logger, wrappedErr, statusCode)
}

// handleError determines the appropriate status code based on error type and writes the response.
// It checks for sentinel errors (ErrNotFound, ErrInvalidRequest, etc.) to determine status codes.
func handleError(w http.ResponseWriter, logger *zap.SugaredLogger, err error, context string) {
	wrappedErr := errors.Wrap(err, context)

	statusCode := http.StatusInternalServerError
	switch {
	case errors.IsNotFoundError(err):
		statusCode = http.StatusNotFound
	case errors.Is(err, errors.ErrInvalidRequest):
		statusCode = http.StatusBadRequest
	case errors.Is(err, errors.ErrUnauthorized):
		statusCode = http.StatusUnauthorized
	case errors.Is(err, errors.ErrForbidden):
		statusCode = http.StatusForbidden
	}

	writeRichError(w, logger, wrappedErr, statusCode)
}

// readJSON reads and decodes a JSON request body
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return errors.Wrap(err, "failed to decode JSON request body")
	}
	return nil
}

// requireMethod checks if the request method matches the expected method
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return false
	}
	return true
}

// requireMethods checks if the request method matches one of the expected methods
func requireMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	return false
}

// Note: extractPathParts() moved to util.go
// Note: shortID() removed - we show full IDs, never truncate
