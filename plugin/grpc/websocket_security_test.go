package grpc

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestCreateOriginChecker_AllowAllOrigins(t *testing.T) {
	config := WebSocketConfig{
		AllowAllOrigins: true,
	}

	checker := CreateOriginChecker(config, nil)

	// Test various origins - all should be allowed
	tests := []struct {
		name   string
		origin string
	}{
		{"http origin", "http://example.com"},
		{"https origin", "https://secure.example.com"},
		{"localhost", "http://localhost:3000"},
		{"empty origin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			assert.True(t, checker(req), "Should allow origin: %s", tt.origin)
		})
	}
}

func TestCreateOriginChecker_ExactMatch(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins: []string{
			"http://localhost:3000",
			"https://example.com",
		},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"allowed localhost", "http://localhost:3000", true},
		{"allowed https", "https://example.com", true},
		{"different port", "http://localhost:8080", false},
		{"different domain", "http://evil.com", false},
		{"no origin header", "", false}, // Empty origin rejected unless from localhost (see TestCreateOriginChecker_EmptyOrigin_Security)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			assert.Equal(t, tt.expected, checker(req), "Origin: %s", tt.origin)
		})
	}
}

func TestCreateOriginChecker_WildcardMatch(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins: []string{
			"http://localhost:*",
			"https://*.example.com",
		},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"localhost any port", "http://localhost:3000", true},
		{"localhost port 8080", "http://localhost:8080", true},
		{"subdomain wildcard", "https://api.example.com", true},
		{"subdomain wildcard 2", "https://app.example.com", true},
		{"different domain", "https://example.org", false},
		{"http on https wildcard", "http://api.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			req.Header.Set("Origin", tt.origin)
			assert.Equal(t, tt.expected, checker(req), "Origin: %s", tt.origin)
		})
	}
}

func TestCreateOriginChecker_StarWildcard(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins:  []string{"*"},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://anywhere.com")
	assert.True(t, checker(req))
}

func TestCreateOriginChecker_WithLogging(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := WebSocketConfig{
		AllowedOrigins:  []string{"http://localhost:3000"},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, logger)

	// Rejected origin should be logged
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://evil.com")
	req.RemoteAddr = "192.168.1.1:1234"

	assert.False(t, checker(req))
	// Logger output is captured by zaptest
}

func TestDefaultWebSocketConfig(t *testing.T) {
	config := DefaultWebSocketConfig()

	assert.False(t, config.AllowAllOrigins, "Should not allow all origins by default")
	assert.False(t, config.AllowCredentials, "Should not allow credentials by default")
	assert.NotEmpty(t, config.AllowedOrigins, "Should have default allowed origins")

	// Verify localhost patterns are included
	assert.Contains(t, config.AllowedOrigins, "http://localhost:*")
	assert.Contains(t, config.AllowedOrigins, "http://127.0.0.1:*")
}

func TestAddSecurityHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	AddSecurityHeaders(w)

	headers := w.Header()

	tests := []struct {
		header   string
		expected string
	}{
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"Content-Security-Policy", "default-src 'self'"},
		{"X-XSS-Protection", "1; mode=block"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			assert.Equal(t, tt.expected, headers.Get(tt.header))
		})
	}
}

func TestWebSocketConfig_RestrictiveDefault(t *testing.T) {
	// Verify that default config is secure (restrictive)
	config := DefaultWebSocketConfig()

	checker := CreateOriginChecker(config, nil)

	// Should reject non-localhost origins
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://example.com")
	assert.False(t, checker(req), "Should reject non-localhost by default")

	// Should allow localhost
	req.Header.Set("Origin", "http://localhost:8080")
	assert.True(t, checker(req), "Should allow localhost by default")
}

func TestOriginChecker_EdgeCases(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins: []string{
			"http://localhost:3000",
		},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"case sensitive", "HTTP://LOCALHOST:3000", false},
		{"trailing slash", "http://localhost:3000/", false},
		{"with path", "http://localhost:3000/app", false},
		{"empty string", "", true}, // No origin header
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			result := checker(req)
			assert.Equal(t, tt.expected, result, "Origin: %s", tt.origin)
		})
	}
}

func TestWebSocketConfig_MultiplePatterns(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins: []string{
			"http://localhost:*",
			"http://127.0.0.1:*",
			"https://*.myapp.com",
			"https://myapp.com",
		},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"localhost 3000", "http://localhost:3000", true},
		{"localhost 8080", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:9000", true},
		{"subdomain", "https://api.myapp.com", true},
		{"main domain", "https://myapp.com", true},
		{"wrong subdomain", "https://api.otherapp.com", false},
		{"http on https domain", "http://api.myapp.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			req.Header.Set("Origin", tt.origin)
			assert.Equal(t, tt.expected, checker(req), "Origin: %s", tt.origin)
		})
	}
}

func TestCreateOriginChecker_EmptyOrigin_Security(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := WebSocketConfig{
		AllowedOrigins:  []string{"http://localhost:3000"},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, logger)

	tests := []struct {
		name       string
		remoteAddr string
		expected   bool
		reason     string
	}{
		{
			name:       "localhost IPv4",
			remoteAddr: "127.0.0.1:54321",
			expected:   true,
			reason:     "Localhost IPv4 should be trusted",
		},
		{
			name:       "localhost IPv6",
			remoteAddr: "[::1]:54321",
			expected:   true,
			reason:     "Localhost IPv6 should be trusted",
		},
		{
			name:       "localhost hostname",
			remoteAddr: "localhost:54321",
			expected:   true,
			reason:     "Localhost hostname should be trusted",
		},
		{
			name:       "remote IPv4",
			remoteAddr: "192.168.1.100:54321",
			expected:   false,
			reason:     "Remote IPv4 should be rejected",
		},
		{
			name:       "remote IPv6",
			remoteAddr: "[2001:db8::1]:54321",
			expected:   false,
			reason:     "Remote IPv6 should be rejected",
		},
		{
			name:       "public IP",
			remoteAddr: "8.8.8.8:54321",
			expected:   false,
			reason:     "Public IP should be rejected",
		},
		{
			name:       "private network",
			remoteAddr: "10.0.0.5:54321",
			expected:   false,
			reason:     "Private network IP should be rejected",
		},
		{
			name:       "loopback range start",
			remoteAddr: "127.0.0.0:54321",
			expected:   true,
			reason:     "127.0.0.0 is in loopback range",
		},
		{
			name:       "loopback range end",
			remoteAddr: "127.255.255.255:54321",
			expected:   true,
			reason:     "127.255.255.255 is in loopback range",
		},
		{
			name:       "invalid format",
			remoteAddr: "not-a-valid-address",
			expected:   false,
			reason:     "Invalid address format should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			// Don't set Origin header - testing empty origin behavior
			req.RemoteAddr = tt.remoteAddr

			result := checker(req)
			assert.Equal(t, tt.expected, result, tt.reason)
		})
	}
}

func TestCreateOriginChecker_WithOrigin_IgnoresRemoteAddr(t *testing.T) {
	config := WebSocketConfig{
		AllowedOrigins:  []string{"http://localhost:3000"},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, nil)

	// When Origin header is present, RemoteAddr shouldn't matter
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.RemoteAddr = "8.8.8.8:54321" // Public IP

	// Should pass because Origin matches, regardless of RemoteAddr
	assert.True(t, checker(req), "Valid origin should pass regardless of RemoteAddr")
}

func TestCreateOriginChecker_AllowAllOrigins_BypassesCheck(t *testing.T) {
	config := WebSocketConfig{
		AllowAllOrigins: true,
	}

	checker := CreateOriginChecker(config, nil)

	tests := []struct {
		name       string
		origin     string
		remoteAddr string
	}{
		{"no origin, remote IP", "", "8.8.8.8:54321"},
		{"evil origin, remote IP", "http://evil.com", "8.8.8.8:54321"},
		{"no origin, localhost", "", "127.0.0.1:54321"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			req.RemoteAddr = tt.remoteAddr

			assert.True(t, checker(req), "AllowAllOrigins should allow everything")
		})
	}
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		// IPv4
		{"127.0.0.1", true},
		{"127.0.0.0", true},
		{"127.255.255.255", true},
		{"127.1.2.3", true}, // Any 127.x.x.x
		{"128.0.0.1", false},
		{"192.168.1.1", false},

		// IPv6
		{"::1", true},
		{"::ffff:127.0.0.1", true}, // IPv4-mapped
		{"2001:db8::1", false},
		{"::", false},

		// Hostnames
		{"localhost", true},
		{"ip6-localhost", true},
		{"ip6-loopback", true},
		{"example.com", false},
		{"", false},

		// Edge cases
		{"LOCALHOST", false}, // Case sensitive
		{"local", false},
		{"127", false}, // Incomplete IP
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := isLocalhost(tt.host)
			assert.Equal(t, tt.expected, result, "Host: %s", tt.host)
		})
	}
}

func TestCreateOriginChecker_EmptyOrigin_EdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := WebSocketConfig{
		AllowedOrigins:  []string{"http://localhost:3000"},
		AllowAllOrigins: false,
	}

	checker := CreateOriginChecker(config, logger)

	tests := []struct {
		name       string
		origin     string
		remoteAddr string
		expected   bool
	}{
		{
			name:       "Origin set, takes precedence",
			origin:     "http://localhost:3000",
			remoteAddr: "8.8.8.8:54321", // Would fail if empty origin checked
			expected:   true,
		},
		{
			name:       "IPv6 with brackets",
			origin:     "",
			remoteAddr: "[::ffff:127.0.0.1]:54321", // IPv4-mapped IPv6
			expected:   true,
		},
		{
			name:       "No port in RemoteAddr",
			origin:     "",
			remoteAddr: "127.0.0.1", // Missing port
			expected:   false,       // Should reject due to parse error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			req.RemoteAddr = tt.remoteAddr

			result := checker(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}
