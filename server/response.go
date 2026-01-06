package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/errors"
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

// readJSON reads and decodes a JSON request body
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return err
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
// Note: isNotFoundError() replaced with IsNotFoundError() in errors.go
// Note: shortID() removed - we show full IDs, never truncate
