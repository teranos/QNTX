package grpc

import (
	"net/http"
	"path/filepath"
)

// WebSocketConfig defines security policy for WebSocket connections
type WebSocketConfig struct {
	// AllowedOrigins is a list of allowed origin patterns
	// Supports wildcards: "http://localhost:*", "https://*.example.com"
	AllowedOrigins []string

	// AllowAllOrigins allows any origin (development only - insecure)
	AllowAllOrigins bool

	// AllowCredentials permits credentials in WebSocket requests
	AllowCredentials bool
}

// DefaultWebSocketConfig returns a secure default configuration
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		AllowedOrigins: []string{
			"http://localhost:*",
			"http://127.0.0.1:*",
		},
		AllowAllOrigins:  false,
		AllowCredentials: false,
	}
}

// CreateOriginChecker creates a CheckOrigin function for websocket.Upgrader
func CreateOriginChecker(config WebSocketConfig, logger interface{}) func(*http.Request) bool {
	// Logger interface for optional logging
	type sugaredLogger interface {
		Warnw(msg string, keysAndValues ...interface{})
	}

	var log sugaredLogger
	if l, ok := logger.(sugaredLogger); ok {
		log = l
	}

	return func(r *http.Request) bool {
		// Allow all origins if configured (dev mode)
		if config.AllowAllOrigins {
			return true
		}

		origin := r.Header.Get("Origin")

		// No origin header could mean same-origin request
		// Some WebSocket clients don't send Origin
		if origin == "" {
			return true
		}

		// Check against allowed origins
		for _, allowed := range config.AllowedOrigins {
			// Exact match
			if origin == allowed {
				return true
			}

			// Wildcard match using filepath.Match (supports * and ?)
			if matched, _ := filepath.Match(allowed, origin); matched {
				return true
			}

			// Special case: allow "*" to match anything
			if allowed == "*" {
				return true
			}
		}

		// Origin not allowed
		if log != nil {
			log.Warnw("WebSocket origin rejected",
				"origin", origin,
				"remote_addr", r.RemoteAddr,
				"path", r.URL.Path,
				"allowed_origins", config.AllowedOrigins,
			)
		}

		return false
	}
}

// AddSecurityHeaders adds security headers to WebSocket HTTP responses
func AddSecurityHeaders(w http.ResponseWriter) {
	// Prevent clickjacking
	w.Header().Set("X-Frame-Options", "DENY")

	// Prevent MIME type sniffing
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Basic CSP - plugins can override if needed
	w.Header().Set("Content-Security-Policy", "default-src 'self'")

	// Prevent XSS in older browsers
	w.Header().Set("X-XSS-Protection", "1; mode=block")
}
