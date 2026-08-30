// Package httputil provides shared HTTP handler utilities for QNTX plugins.
//
// Plugins that serve HTTP endpoints (glyph content, API handlers) share
// common patterns: JSON request/response encoding, error responses, and
// HTML escaping for server-rendered glyph content. This package eliminates
// the duplication across qntx-atproto, ix-json, etc.
package httputil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// WriteJSON writes a JSON response with the given status code. The status is
// already on the wire by the time the body fails, so the caller cannot fix it —
// but it is the only place that knows enough to record it.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		return fmt.Errorf("the %d response body was not delivered: %w", status, err)
	}
	return nil
}

// WriteError writes a JSON error response: {"error": message}.
func WriteError(w http.ResponseWriter, status int, message string) error {
	return WriteJSON(w, status, map[string]string{"error": message})
}

// ReadJSON decodes a JSON request body into v.
// On failure, writes a 400 error response and returns the error.
func ReadJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		// The 400 failing to send does not change why the request was refused.
		if writeErr := WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err)); writeErr != nil {
			return fmt.Errorf("%w (and the 400 saying so was not delivered: %w)", err, writeErr)
		}
		return err
	}
	return nil
}

// EscapeHTML escapes special HTML characters in s.
// Use for text content inside HTML elements.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
