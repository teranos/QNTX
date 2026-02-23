package qntxatproto

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Can't change status code after WriteHeader, log the error
		// Client receives partial JSON with 200/whatever status was set
		http.Error(w, "JSON encoding failed", http.StatusInternalServerError)
	}
}

// readJSON reads and decodes a JSON request body.
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return err
	}
	return nil
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		// Can't change status code after WriteHeader
		http.Error(w, "JSON encoding failed", http.StatusInternalServerError)
	}
}
