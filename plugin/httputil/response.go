// Package httputil provides shared HTTP handler utilities for QNTX plugins.
//
// Plugins that serve HTTP endpoints (glyph content, API handlers) share
// common patterns: JSON request/response encoding, error responses, and
// HTML escaping for server-rendered glyph content. This package eliminates
// the duplication across qntx-atproto, qntx-ix-json, qntx-code, etc.
package httputil

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// WriteError writes a JSON error response: {"error": message}.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// ReadJSON decodes a JSON request body into v.
// On failure, writes a 400 error response and returns the error.
func ReadJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
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

// EscapeHTMLAttr escapes s for safe use in HTML attribute values.
func EscapeHTMLAttr(s string) string {
	return EscapeHTML(s)
}
