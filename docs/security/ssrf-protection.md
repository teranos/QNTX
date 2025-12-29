# HTTP Client for User-Controlled URLs

## Why This Exists

**User-controlled URLs are risky.** If your code fetches `http://userInput`, an attacker can provide `http://localhost:6379` (probe internal Redis), `http://169.254.169.254/metadata` (steal AWS credentials), or `http://192.168.1.1/admin` (scan your network).

**SaferClient blocks private IPs and localhost.** It's defense-in-depth, not a security guarantee.

## Rule

Use `SaferClient` for user-controlled URLs. Use regular `http.Client` for trusted URLs.

```go
// ❌ Risky for user input
http.Get(userURL)

// ✅ Use this instead
client := httpclient.NewSaferClient(30 * time.Second)
client.Get(userURL)
```

## What It Blocks

**Tested and verified:**
- Private IPv4: 10.x, 192.168.x, 172.16-31.x, 127.x
- Cloud metadata IP: 169.254.169.254
- Localhost: `localhost`, `*.localhost`
- @ character in URLs (credential injection)
- file://, ftp://, gopher:// schemes
- Redirects to private IPs
- **All IPv6 addresses** (disabled until comprehensively tested)

**Not thoroughly tested:**
- DNS rebinding attacks

**IPv6 Policy**: All IPv6 addresses are rejected until proper test coverage exists for edge cases like IPv4-mapped addresses (::ffff:127.0.0.1), zone IDs, and other IPv6-specific attack vectors. This is a conservative approach - legitimate IPv6 use cases will fail.

## Usage

### Basic

```go
import "github.com/teranos/QNTX/internal/httpclient"

client := httpclient.NewSaferClient(30 * time.Second)
resp, err := client.Get(userURL)
if err != nil {
    // Blocked or network error
    return err
}
defer resp.Body.Close()
```

### HTTPS-Only

```go
maxRedirects := 5
opts := httpclient.SaferClientOptions{
    AllowedSchemes: []string{"https"},
    MaxRedirects:   &maxRedirects,
}
client := httpclient.NewSaferClientWithOptions(30*time.Second, opts)
```

### Tests (Disable Protection)

```go
// ⚠️ Tests only
allowPrivateIP := false
opts := httpclient.SaferClientOptions{
    BlockPrivateIP: &allowPrivateIP,
}
client := httpclient.NewSaferClientWithOptions(30*time.Second, opts)
```

## How It Works

1. **Pre-validation**: Checks URL scheme, blocks @ character
2. **DNS resolution**: Resolves hostname to IPs at dial time
3. **IP validation**: Blocks if any resolved IP is private
4. **Redirect validation**: Repeats checks for each redirect

**Why dial-time validation?** Attackers can use DNS to bypass hostname-only filters. `evil.com` might resolve to `127.0.0.1`. We check the actual IPs before connecting.

**Limitation**: Sophisticated DNS rebinding (where DNS changes between validation and connection) isn't actively tested. The dial-time check mitigates this but isn't foolproof.

## Testing

```bash
go test -v github.com/teranos/QNTX/internal/httpclient
```

**What's covered**: RFC1918 ranges, loopback, AWS metadata IP, @ injection, scheme blocking, redirect chains

**What's missing**: IPv6 edge cases, DNS rebinding simulation, timing attacks

## When to Use

**Use SaferClient for:**
- Webhook URLs from users
- Data ingestion from user-provided sources
- Link previews, unfurling
- Any HTTP request where URL comes from outside your control

**Use regular http.Client for:**
- Internal service-to-service calls
- Hardcoded URLs in your code
- URLs from trusted configuration

**Don't rely on SaferClient for:**
- Certified security requirements
- Production systems with sophisticated attackers
- IPv6-heavy environments (without adding tests first)

## Improving It

If you need stronger protection:
1. Add IPv6 test cases (especially IPv4-mapped addresses)
2. Simulate DNS rebinding attacks
3. Test against actual SSRF payloads from security research
4. Consider allowlist (only permit known-safe domains) instead of blocklist

The current implementation is better than `http.Get(userURL)`, but it's not enterprise-grade SSRF protection.

## References

- [OWASP SSRF Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- Code: `internal/httpclient/safer_client.go`
- Tests: `internal/httpclient/safer_client_test.go`
