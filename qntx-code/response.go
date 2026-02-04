package qntxcode

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// readJSON reads and decodes a JSON request body
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return err
	}
	return nil
}

// requireMethod checks if the request method matches the expected method
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}
