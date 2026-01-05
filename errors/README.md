# Error Handling in QNTX

QNTX uses `github.com/teranos/QNTX/errors`, which wraps [cockroachdb/errors](https://github.com/cockroachdb/errors) for production-grade error handling.

## Why cockroachdb/errors?

- **Stack traces**: Automatic, lightweight stack capture
- **Rich context**: Hints, details, safe PII handling
- **Network portable**: Encode/decode errors across distributed systems
- **Battle-tested**: Production use in CockroachDB
- **Sentry integration**: Built-in error reporting

## Basic Usage

### Creating Errors

```go
// New error with stack trace
err := errors.New("database connection failed")

// Formatted error
err := errors.Newf("timeout after %d seconds", timeout)
```

### Wrapping Errors

**Always wrap errors to add context at each layer:**

```go
if err := db.Query(...); err != nil {
    return errors.Wrap(err, "failed to query users")
}

// With formatting
return errors.Wrapf(err, "failed to process user %d", userID)
```

### Checking Errors

```go
// Check for specific error
if errors.Is(err, sql.ErrNoRows) {
    return errors.Wrap(err, "user not found")
}

// Extract custom error type
var netErr *net.OpError
if errors.As(err, &netErr) {
    // Handle network error
}
```

## User-Facing Messages

### Hints

Hints provide actionable guidance to users:

```go
err := errors.New("connection timeout")
err = errors.WithHint(err, "try increasing the timeout value in config")
err = errors.WithHintf(err, "or check network connectivity to %s", host)

// Retrieve hints
hints := errors.GetAllHints(err)
```

### Details

Details add technical context for debugging:

```go
err := errors.WithDetail(err, "attempted 3 retries with exponential backoff")
err = errors.WithDetailf(err, "last attempt at %s", timestamp)

details := errors.GetAllDetails(err)
```

## PII Protection

Use `WithSafeDetails` for logging sensitive data:

```go
// Safe: userID is marked safe, email is redacted
err = errors.WithSafeDetails(err, "user_id=%d email=%s", userID, email)

// When formatted for logs, email will be redacted
log.Error(err) // Shows: "user_id=123 email=×××"
```

## Error Formatting

```go
err := errors.Wrap(baseErr, "failed to save user")

// Simple message
fmt.Println(err)
// Output: failed to save user: connection timeout

// Full context with stack trace
fmt.Printf("%+v\n", err)
// Output:
// failed to save user
// (1) attached stack trace
//   -- stack trace:
//   | main.saveUser
//   |     /path/to/file.go:123
//   | main.main
//   |     /path/to/file.go:45
// Wraps: (2) connection timeout
// Error types: (1) *withStack (2) *leafError
```

## Sentinel Errors

Define package-level sentinel errors:

```go
package db

import "github.com/teranos/QNTX/errors"

var (
    ErrNotFound = errors.New("not found")
    ErrClosed   = errors.New("database closed")
)

// Usage
if errors.Is(err, db.ErrNotFound) {
    // handle not found
}
```

## Best Practices

### Do

✓ Wrap errors at every layer
```go
return errors.Wrap(err, "failed to save attestation")
```

✓ Add context about what was being attempted
```go
return errors.Wrapf(err, "failed to connect to %s", address)
```

✓ Use hints for user guidance
```go
return errors.WithHint(err, "check database credentials")
```

✓ Check errors with `errors.Is` and `errors.As`
```go
if errors.Is(err, sql.ErrNoRows) { ... }
```

### Don't

✗ Return bare errors
```go
return err // Missing context
```

✗ Log AND return errors (handle errors once)
```go
log.Error(err)
return err // Causes duplicate logging
```

✗ Create errors with fmt.Errorf
```go
return fmt.Errorf("failed: %w", err) // Use errors.Wrap
```

## Advanced Features

### Context Tags

Attach key-value pairs from context.Context:

```go
err = errors.WithContextTags(err, ctx)
```

### Network Encoding

Transmit errors across network boundaries:

```go
// Encode for transmission
encoded := errors.EncodeError(ctx, err)

// Decode on receiver
decoded := errors.DecodeError(ctx, encoded)
```

### Domains

Mark errors with package domains:

```go
err = errors.WithDomain(err, errors.PackageDomain())
domain := errors.GetDomain(err)
```

## Full Documentation

See [cockroachdb/errors documentation](https://pkg.go.dev/github.com/cockroachdb/errors) for complete API reference.
