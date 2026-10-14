package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	appcfg "github.com/teranos/QNTX/internal/config"
)

// getAxUpgrader creates a WebSocket upgrader with origin checking from config
func getAxUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  2048,
		WriteBufferSize: 2048,
		CheckOrigin:     checkOrigin,
	}
}

// checkOrigin validates WebSocket origin against configured allowed origins.
//
// A missing Origin header is rejected. Every legitimate client — browsers,
// Tauri, and cross-origin JS — sends an Origin. Raw clients that omit the
// header would otherwise bypass origin validation entirely; machine access
// belongs on the bearer token path (ADR-025) served over HTTP, not raw WS.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	// Load allowed origins from cfg (with defaults)
	cfg, err := appcfg.Load()
	if err != nil {
		// If config fails to load, use secure defaults (localhost only + Tauri)
		return matchOrigin(origin, "http://localhost") ||
			matchOrigin(origin, "https://localhost") ||
			matchOrigin(origin, "tauri://localhost") ||
			matchOrigin(origin, "http://tauri.localhost") ||
			matchOrigin(origin, "https://tauri.localhost")
	}

	// Get allowed origins (includes defaults if not configured)
	allowedOrigins := cfg.GetServerAllowedOrigins()

	for _, allowedOrigin := range allowedOrigins {
		if matchOrigin(origin, allowedOrigin) {
			return true
		}
	}

	return false
}

// matchOrigin checks if origin matches an allowed origin (scheme+host).
// Allows any port: "http://localhost" matches "http://localhost:8770"
// but NOT "http://localhost.evil.com" (the old prefix matching did).
func matchOrigin(origin, allowed string) bool {
	if !strings.HasPrefix(origin, allowed) {
		return false
	}
	// After the prefix, must be end-of-string or ":" (port separator)
	rest := origin[len(allowed):]
	return rest == "" || rest[0] == ':'
}

// extractPathParts extracts path segments after removing a prefix
func extractPathParts(urlPath, prefix string) []string {
	return strings.Split(strings.TrimPrefix(urlPath, prefix), "/")
}
