package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teranos/QNTX/errors"
)

// SaferClient wraps http.Client with SSRF protection
type SaferClient struct {
	*http.Client
	allowedSchemes []string
	blockPrivateIP bool
	maxRedirects   int
}

// NewSaferClient creates an HTTP client with SSRF protection
func NewSaferClient(timeout time.Duration) *SaferClient {
	client := &SaferClient{
		Client: &http.Client{
			Timeout: timeout,
		},
		allowedSchemes: []string{"http", "https"},
		blockPrivateIP: true,
		maxRedirects:   10,
	}

	// Set up redirect policy with SSRF protection
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Enforce max redirects
		if len(via) >= client.maxRedirects {
			return errors.Newf("stopped after %d redirects", client.maxRedirects)
		}

		// Validate redirect URL
		if err := client.validateURL(req.URL); err != nil {
			return errors.Wrap(err, "redirect blocked")
		}

		return nil
	}

	// Set up custom dialer with private IP blocking
	if client.blockPrivateIP {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Extract host from addr
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, errors.Wrap(err, "invalid address")
				}

				// Resolve IP address
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to resolve host %q", host)
				}

				// Check if any resolved IP is private
				for _, ip := range ips {
					if isPrivateIP(ip) {
						return nil, errors.Newf("private IP address blocked: %s", ip)
					}
				}

				// Use standard dialer
				return dialer.DialContext(ctx, network, addr)
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	return client
}

// NewSaferClientWithOptions creates an HTTP client with custom SSRF protection options
func NewSaferClientWithOptions(timeout time.Duration, opts SaferClientOptions) *SaferClient {
	// Build client with options
	blockPrivateIP := true
	if opts.BlockPrivateIP != nil {
		blockPrivateIP = *opts.BlockPrivateIP
	}

	maxRedirects := 10
	if opts.MaxRedirects != nil {
		maxRedirects = *opts.MaxRedirects
	}

	allowedSchemes := []string{"http", "https"}
	if opts.AllowedSchemes != nil {
		allowedSchemes = opts.AllowedSchemes
	}

	client := &SaferClient{
		Client: &http.Client{
			Timeout: timeout,
		},
		allowedSchemes: allowedSchemes,
		blockPrivateIP: blockPrivateIP,
		maxRedirects:   maxRedirects,
	}

	// Set up redirect policy with SSRF protection
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Enforce max redirects
		if len(via) >= client.maxRedirects {
			return errors.Newf("stopped after %d redirects", client.maxRedirects)
		}

		// Validate redirect URL
		if err := client.validateURL(req.URL); err != nil {
			return errors.Wrap(err, "redirect blocked")
		}

		return nil
	}

	// Set up custom dialer with private IP blocking (only if enabled)
	if blockPrivateIP {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Extract host from addr
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, errors.Wrap(err, "invalid address")
				}

				// Resolve IP address
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, errors.Wrap(err, "failed to resolve host")
				}

				// Check if any resolved IP is private
				for _, ip := range ips {
					if isPrivateIP(ip) {
						return nil, errors.Newf("private IP address blocked: %s", ip)
					}
				}

				// Use standard dialer
				return dialer.DialContext(ctx, network, addr)
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	return client
}

// SaferClientOptions allows customization of SSRF protection
type SaferClientOptions struct {
	AllowedSchemes []string // Default: ["http", "https"]
	MaxRedirects   *int     // Default: 10
	BlockPrivateIP *bool    // Default: true
}

// validateURL validates URL for SSRF protection before making request
func (c *SaferClient) validateURL(u *url.URL) error {
	// Check scheme
	scheme := strings.ToLower(u.Scheme)
	allowed := false
	for _, allowedScheme := range c.allowedSchemes {
		if scheme == allowedScheme {
			allowed = true
			break
		}
	}
	if !allowed {
		return errors.Newf("scheme %q not allowed (allowed: %v)", scheme, c.allowedSchemes)
	}

	// Check for suspicious patterns in URL
	if strings.Contains(u.String(), "@") {
		// Could be credential injection or URL confusion: http://evil.com@localhost/
		return errors.New("URL contains @ character (potential SSRF attempt)")
	}

	// Parse hostname
	hostname := u.Hostname()
	if hostname == "" {
		return errors.New("URL missing hostname")
	}

	// Block localhost variants (only if blocking is enabled)
	if c.blockPrivateIP {
		if isLocalhost(hostname) {
			return errors.New("localhost access blocked")
		}

		// Block private IPs in hostname (DNS rebinding protection handled by DialContext)
		if ip := net.ParseIP(hostname); ip != nil && isPrivateIP(ip) {
			return errors.Newf("private IP address blocked: %s", hostname)
		}
	}

	return nil
}

// ValidateURL validates a URL string before creating a request
func (c *SaferClient) ValidateURL(urlStr string) (*url.URL, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, errors.Wrap(err, "invalid URL")
	}

	if err := c.validateURL(u); err != nil {
		return nil, err
	}

	return u, nil
}

// isPrivateIP checks if an IP is in private/special use ranges
func isPrivateIP(ip net.IP) bool {
	// RFC 1918 private networks
	privateBlocks := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},     // 10.0.0.0/8
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},  // 172.16.0.0/12
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)}, // 192.168.0.0/16
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},    // 127.0.0.0/8 (loopback)
		{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)}, // 169.254.0.0/16 (link-local)
		{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)},      // 0.0.0.0/8
		{IP: net.IPv4(224, 0, 0, 0), Mask: net.CIDRMask(4, 32)},    // 224.0.0.0/4 (multicast)
		{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)},    // 240.0.0.0/4 (reserved)
	}

	// Check IPv4
	if ip4 := ip.To4(); ip4 != nil {
		for _, block := range privateBlocks {
			if block.Contains(ip4) {
				return true
			}
		}
		return false
	}

	// Reject all IPv6 addresses until properly tested
	// IPv6 has edge cases (IPv4-mapped, zone IDs, etc.) that aren't comprehensively tested
	// See docs/security/ssrf-protection.md for rationale
	if len(ip) == net.IPv6len {
		return true
	}

	return false
}

// isLocalhost checks for localhost variants
func isLocalhost(hostname string) bool {
	hostname = strings.ToLower(hostname)
	return hostname == "localhost" ||
		hostname == "localhost.localdomain" ||
		strings.HasSuffix(hostname, ".localhost")
}

// Get is a convenience wrapper for http.Get with SSRF protection
func (c *SaferClient) Get(urlStr string) (*http.Response, error) {
	if _, err := c.ValidateURL(urlStr); err != nil {
		return nil, err
	}
	return c.Client.Get(urlStr)
}

// Do executes an HTTP request with SSRF protection
// For POST requests, use http.NewRequest() then call Do()
func (c *SaferClient) Do(req *http.Request) (*http.Response, error) {
	if err := c.validateURL(req.URL); err != nil {
		return nil, errors.Wrap(err, "request blocked by SSRF protection")
	}
	return c.Client.Do(req)
}

// WrapClient wraps an existing http.Client in a SaferClient without SSRF protection.
// ⚠️ WARNING: Only use this in tests where you need to use httptest.NewServer on localhost.
func WrapClient(client *http.Client) *SaferClient {
	return &SaferClient{
		Client:         client,
		allowedSchemes: []string{"http", "https"},
		blockPrivateIP: false, // Disabled for test clients
		maxRedirects:   10,
	}
}
