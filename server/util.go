package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
)

// getAxUpgrader creates a WebSocket upgrader with origin checking from config
func getAxUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  2048,
		WriteBufferSize: 2048,
		CheckOrigin:     checkOrigin,
	}
}

// checkOrigin validates WebSocket origin against configured allowed origins
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// Allow requests with no origin header (e.g., direct WebSocket clients, testing)
	if origin == "" {
		return true
	}

	// Load allowed origins from cfg (with defaults)
	cfg, err := appcfg.Load()
	if err != nil {
		// If config fails to load, use secure defaults (localhost only + Tauri)
		return matchOrigin(origin, "http://localhost") ||
			matchOrigin(origin, "https://localhost") ||
			matchOrigin(origin, "tauri://localhost")
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

// isPortAvailable checks if a port is available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close() // Error ignored: best-effort port check, caller will retry on actual bind
	return true
}

// findAvailablePort tries to find an available port starting from the requested port
// It tries: requested port, preferred fallbacks (8770, 7878), then high-range ports (56787-56796)
func findAvailablePort(requestedPort int) (int, error) {
	// Try the requested port first
	if isPortAvailable(requestedPort) {
		return requestedPort, nil
	}

	// Preferred fallback ports for development and production
	preferredPorts := []int{appcfg.DefaultServerPort, appcfg.FallbackServerPort}

	// Try preferred ports if they differ from requested
	for _, port := range preferredPorts {
		if port != requestedPort && isPortAvailable(port) {
			return port, nil
		}
	}

	// Try high-range fallback ports as last resort (56787-56796)
	fallbackStart := 56787
	for i := 0; i < 10; i++ {
		port := fallbackStart + i
		if isPortAvailable(port) {
			return port, nil
		}
	}

	return 0, errors.Newf("no available ports found (tried %d, %d, %d, and range 56787-56796)", requestedPort, appcfg.DefaultServerPort, appcfg.FallbackServerPort)
}

// extractPathParts extracts path segments after removing a prefix
func extractPathParts(urlPath, prefix string) []string {
	return strings.Split(strings.TrimPrefix(urlPath, prefix), "/")
}
