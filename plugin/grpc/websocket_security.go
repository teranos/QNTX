package grpc

import (
	"net"
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
		Debugw(msg string, keysAndValues ...interface{})
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

		// SECURITY: Empty origin headers are only allowed from localhost
		// Some WebSocket clients (like wscat, websocat) don't send Origin header
		// But we should only trust this from local connections
		if origin == "" {
			// Extract host from RemoteAddr (format: "IP:port" or "[IPv6]:port")
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// If we can't parse RemoteAddr, reject for safety
				if log != nil {
					log.Warnw("WebSocket rejected - invalid RemoteAddr format",
						"remote_addr", r.RemoteAddr,
						"error", err,
					)
				}
				return false
			}

			// Check if connection is from localhost
			if isLocalhost(host) {
				if log != nil {
					log.Debugw("WebSocket accepted - empty origin from localhost",
						"remote_addr", r.RemoteAddr,
						"host", host,
					)
				}
				return true
			}

			// Reject empty origin from remote hosts
			if log != nil {
				log.Warnw("WebSocket rejected - empty origin from remote host",
					"remote_addr", r.RemoteAddr,
					"host", host,
				)
			}
			return false
		}

		// Check against allowed origins
		for _, allowed := range config.AllowedOrigins {
			// Exact match
			if origin == allowed {
				if log != nil {
					log.Debugw("WebSocket accepted - exact origin match",
						"origin", origin,
						"pattern", allowed,
						"remote_addr", r.RemoteAddr,
					)
				}
				return true
			}

			// Wildcard match using filepath.Match (supports * and ?)
			if matched, err := filepath.Match(allowed, origin); err == nil && matched {
				if log != nil {
					log.Debugw("WebSocket accepted - wildcard origin match",
						"origin", origin,
						"pattern", allowed,
						"remote_addr", r.RemoteAddr,
					)
				}
				return true
			}

			// Special case: allow "*" to match anything
			if allowed == "*" {
				if log != nil {
					log.Debugw("WebSocket accepted - wildcard match",
						"origin", origin,
						"remote_addr", r.RemoteAddr,
					)
				}
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

// isLocalhost checks if the given host is a localhost address
func isLocalhost(host string) bool {
	// Parse as IP first
	ip := net.ParseIP(host)
	if ip != nil {
		// Check IPv4 localhost (127.0.0.0/8)
		if ip.IsLoopback() {
			return true
		}
		// Explicitly check 127.0.0.1
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			return true
		}
		// Explicitly check ::1 (IPv6 localhost)
		if ip.Equal(net.IPv6loopback) {
			return true
		}
		return false
	}

	// String comparison for hostname
	switch host {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	default:
		return false
	}
}
