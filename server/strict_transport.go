package server

import (
	"net/http"
	"strings"
)

// strictTransportPolicy is the Strict-Transport-Security a response over https
// carries: a year, and no subdomains, which are not the node's to speak for.
const strictTransportPolicy = "max-age=31536000"

// strictTransport is A2A's "Agents SHOULD enforce HSTS (HTTP Strict Transport
// Security) headers when using HTTP-based bindings" (§13.4). HSTS is a host's,
// not a path's (RFC 6797), so every response to a request that came over
// https carries it, and none over plain http, where it means nothing.
func strictTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", strictTransportPolicy)
		}
		next.ServeHTTP(w, r)
	})
}
