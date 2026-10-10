package grpc

import (
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"

	"go.uber.org/zap"
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
func CreateOriginChecker(config WebSocketConfig, log *zap.SugaredLogger) func(*http.Request) bool {
	return func(r *http.Request) bool {
		// Allow all origins if configured (dev mode)
		if config.AllowAllOrigins {
			return true
		}

		// SECURITY: a request sending no Origin is only allowed from localhost.
		// Some WebSocket clients (like wscat, websocat) don't send Origin header
		// But we should only trust this from local connections
		if _, sent := r.Header["Origin"]; !sent {
			// Extract host from RemoteAddr (format: "IP:port" or "[IPv6]:port")
			host, port, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// If we can't parse RemoteAddr, reject for safety
				log.Warnw("WebSocket rejected - invalid RemoteAddr format",
					"remote_addr", r.RemoteAddr,
					"error", err,
				)
				return false
			}

			// Check if connection is from localhost
			if isLocalhost(host) {
				log.Debugw("WebSocket accepted - no origin from localhost",
					"host", host,
					"port", port,
				)
				return true
			}

			// Reject a request sending no origin from remote hosts
			log.Warnw("WebSocket rejected - no origin from remote host",
				"host", host,
				"port", port,
			)
			return false
		}

		// An Origin is a scheme and a host. One that names neither is refused,
		// whatever the allowed patterns would match.
		origin := r.Header.Get("Origin")
		if !strings.Contains(origin, "://") {
			log.Warnw("WebSocket rejected - origin names no scheme and host",
				"origin", origin,
				"remote_addr", r.RemoteAddr,
			)
			return false
		}

		// Check against allowed origins
		for _, allowed := range config.AllowedOrigins {
			// Exact match
			if origin == allowed {
				log.Debugw("WebSocket accepted - exact origin match",
					"origin", origin,
					"pattern", allowed,
					"remote_addr", r.RemoteAddr,
				)
				return true
			}

			// Wildcard match using filepath.Match (supports * and ?)
			if matched, err := filepath.Match(allowed, origin); err == nil && matched {
				log.Debugw("WebSocket accepted - wildcard origin match",
					"origin", origin,
					"pattern", allowed,
					"remote_addr", r.RemoteAddr,
				)
				return true
			}

			// Special case: allow "*" to match anything
			if allowed == "*" {
				log.Debugw("WebSocket accepted - wildcard match",
					"origin", origin,
					"remote_addr", r.RemoteAddr,
				)
				return true
			}
		}

		// Origin not allowed
		log.Warnw("WebSocket origin rejected",
			"origin", origin,
			"remote_addr", r.RemoteAddr,
			"path", r.URL.Path,
			"allowed_origins", config.AllowedOrigins,
		)
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

// localhostNames are the names a host is localhost by.
var localhostNames = []string{"localhost", "ip6-localhost", "ip6-loopback"}

// isLocalhost checks if the given host is a localhost address: an IP in the
// loopback range, or one of localhostNames.
func isLocalhost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback()
	}
	return slices.Contains(localhostNames, host)
}
