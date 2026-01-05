package httpclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSaferClient(t *testing.T) {
	client := NewSaferClient(30 * time.Second)

	if client == nil {
		t.Fatal("NewSaferClient returned nil")
	}

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.Timeout)
	}

	if client.maxRedirects != 10 {
		t.Errorf("Expected maxRedirects 10, got %d", client.maxRedirects)
	}

	if !client.blockPrivateIP {
		t.Error("Expected blockPrivateIP to be true")
	}
}

func TestValidateURL(t *testing.T) {
	client := NewSaferClient(30 * time.Second)

	tests := []struct {
		name        string
		url         string
		shouldErr   bool
		errContains string
	}{
		// Valid URLs
		{
			name:      "Valid HTTPS URL",
			url:       "https://example.com/path",
			shouldErr: false,
		},
		{
			name:      "Valid HTTP URL",
			url:       "http://example.com",
			shouldErr: false,
		},

		// Invalid schemes
		{
			name:        "File scheme blocked",
			url:         "file:///etc/passwd",
			shouldErr:   true,
			errContains: "scheme",
		},
		{
			name:        "FTP scheme blocked",
			url:         "ftp://example.com",
			shouldErr:   true,
			errContains: "scheme",
		},
		{
			name:        "Gopher scheme blocked",
			url:         "gopher://example.com",
			shouldErr:   true,
			errContains: "scheme",
		},

		// Localhost variants
		{
			name:        "Localhost blocked",
			url:         "http://localhost/admin",
			shouldErr:   true,
			errContains: "localhost",
		},
		{
			name:        "127.0.0.1 blocked",
			url:         "http://127.0.0.1/",
			shouldErr:   true,
			errContains: "private IP",
		},
		{
			name:        "Localhost subdomain blocked",
			url:         "http://admin.localhost/",
			shouldErr:   true,
			errContains: "localhost",
		},

		// Private IPs
		{
			name:        "10.x private network blocked",
			url:         "http://10.0.0.1/",
			shouldErr:   true,
			errContains: "private IP",
		},
		{
			name:        "192.168.x private network blocked",
			url:         "http://192.168.1.1/",
			shouldErr:   true,
			errContains: "private IP",
		},
		{
			name:        "172.16.x private network blocked",
			url:         "http://172.16.0.1/",
			shouldErr:   true,
			errContains: "private IP",
		},
		{
			name:        "Link-local 169.254.x blocked",
			url:         "http://169.254.169.254/metadata",
			shouldErr:   true,
			errContains: "private IP",
		},

		// SSRF attack vectors
		{
			name:        "URL with @ blocked (credential injection)",
			url:         "http://evil.com@localhost/",
			shouldErr:   true,
			errContains: "@",
		},
		{
			name:        "URL with @ blocked (host confusion)",
			url:         "http://user:pass@10.0.0.1/",
			shouldErr:   true,
			errContains: "@",
		},

		// Edge cases
		{
			name:        "Empty hostname",
			url:         "http:///path",
			shouldErr:   true,
			errContains: "hostname",
		},
		{
			name:      "Public IP allowed",
			url:       "http://8.8.8.8/",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ValidateURL(tt.url)

			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for %s, got nil", tt.url)
			}

			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error for %s, got: %v", tt.url, err)
			}

			if tt.shouldErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		// Private IPs
		{"10.0.0.1", "10.0.0.1", true},
		{"10.255.255.255", "10.255.255.255", true},
		{"192.168.0.1", "192.168.0.1", true},
		{"192.168.255.255", "192.168.255.255", true},
		{"172.16.0.1", "172.16.0.1", true},
		{"172.31.255.255", "172.31.255.255", true},
		{"127.0.0.1", "127.0.0.1", true},
		{"127.255.255.255", "127.255.255.255", true},
		{"169.254.0.1", "169.254.0.1", true},
		{"169.254.169.254", "169.254.169.254", true}, // AWS metadata
		{"0.0.0.0", "0.0.0.0", true},
		{"224.0.0.1", "224.0.0.1", true}, // Multicast
		{"240.0.0.1", "240.0.0.1", true}, // Reserved

		// Public IPs
		{"8.8.8.8", "8.8.8.8", false},             // Google DNS
		{"1.1.1.1", "1.1.1.1", false},             // Cloudflare DNS
		{"93.184.216.34", "93.184.216.34", false}, // example.com

		// IPv6
		{"::1", "::1", true},                                   // Loopback
		{"fe80::1", "fe80::1", true},                           // Link-local
		{"fc00::1", "fc00::1", true},                           // ULA
		{"2001:4860:4860::8888", "2001:4860:4860::8888", true}, // Public IPv6 (Google DNS) - blocked
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse IP: %s", tt.ip)
			}

			result := isPrivateIP(ip)
			if result != tt.isPrivate {
				t.Errorf("isPrivateIP(%s) = %v, expected %v", tt.ip, result, tt.isPrivate)
			}
		})
	}
}

func TestRedirectProtection(t *testing.T) {
	// Create a test server that we can control redirects for
	// We'll use a custom client that allows localhost for the initial request
	// but blocks the redirect
	allowLocalhost := false
	client := NewSaferClientWithOptions(5*time.Second, SaferClientOptions{
		BlockPrivateIP: &allowLocalhost,
	})

	// Create a server that redirects to localhost
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost/admin", http.StatusFound)
	}))
	defer redirectServer.Close()

	// Re-enable blocking for the actual test
	client.blockPrivateIP = true

	// Should block redirect to localhost
	resp, err := client.Get(redirectServer.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("Expected error when redirecting to localhost, got nil")
	}

	// Either "redirect blocked" or "localhost" should appear in the error
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "redirect") && !strings.Contains(errMsg, "localhost") && !strings.Contains(errMsg, "private ip") {
		t.Errorf("Expected redirect/localhost/private IP error, got: %v", err)
	}
}

func TestMaxRedirects(t *testing.T) {
	// We need to test max redirects without hitting private IP blocking
	// Create a client that allows localhost temporarily
	allowLocalhost := false
	client := NewSaferClientWithOptions(5*time.Second, SaferClientOptions{
		BlockPrivateIP: &allowLocalhost,
	})

	// Create a server with infinite redirects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirect", http.StatusFound)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("Expected error for too many redirects, got nil")
	}

	if !strings.Contains(err.Error(), "stopped after") && !strings.Contains(err.Error(), "redirects") {
		t.Errorf("Expected redirect limit error, got: %v", err)
	}
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		hostname string
		expected bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"Localhost", true},
		{"localhost.localdomain", true},
		{"admin.localhost", true},
		{"test.localhost", true},
		{"example.com", false},
		{"local", false},
		{"local.host", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			result := isLocalhost(tt.hostname)
			if result != tt.expected {
				t.Errorf("isLocalhost(%q) = %v, expected %v", tt.hostname, result, tt.expected)
			}
		})
	}
}

func TestSaferClientOptions(t *testing.T) {
	maxRedirects := 5
	blockPrivateIP := false
	opts := SaferClientOptions{
		AllowedSchemes: []string{"https"},
		MaxRedirects:   &maxRedirects,
		BlockPrivateIP: &blockPrivateIP,
	}

	client := NewSaferClientWithOptions(30*time.Second, opts)

	if len(client.allowedSchemes) != 1 || client.allowedSchemes[0] != "https" {
		t.Errorf("Expected allowedSchemes [https], got %v", client.allowedSchemes)
	}

	if client.maxRedirects != 5 {
		t.Errorf("Expected maxRedirects 5, got %d", client.maxRedirects)
	}

	if client.blockPrivateIP != false {
		t.Error("Expected blockPrivateIP to be false")
	}

	// Test that HTTP is blocked
	_, err := client.ValidateURL("http://example.com")
	if err == nil {
		t.Error("Expected HTTP to be blocked with HTTPS-only config")
	}
}

func TestDoMethod(t *testing.T) {
	// Test with a client that allows localhost for the test server
	allowLocalhost := false
	client := NewSaferClientWithOptions(5*time.Second, SaferClientOptions{
		BlockPrivateIP: &allowLocalhost,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Valid request to test server (localhost allowed temporarily)
	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Valid request failed: %v", err)
	}
	resp.Body.Close()

	// Now test with blocking enabled
	client2 := NewSaferClient(5 * time.Second)

	// Invalid request (localhost) - should be blocked
	req, err = http.NewRequest("GET", "http://localhost/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err = client2.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("Expected error for localhost request, got nil")
	}

	if !strings.Contains(err.Error(), "SSRF protection") {
		t.Errorf("Expected SSRF protection error, got: %v", err)
	}
}
