package server

import (
	"errors"
	"strings"
)

// Sentinel errors for common cases
var (
	// ErrNotFound indicates the requested resource does not exist
	ErrNotFound = errors.New("not found")

	// ErrInvalidRequest indicates the request was malformed or invalid
	ErrInvalidRequest = errors.New("invalid request")

	// ErrUnauthorized indicates the request lacks proper authentication
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indicates the request is not allowed for this user
	ErrForbidden = errors.New("forbidden")
)

// IsNotFoundError checks if an error is or wraps ErrNotFound
// Also provides backward compatibility with string-based "not found" errors
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check if error is or wraps our sentinel error
	if errors.Is(err, ErrNotFound) {
		return true
	}
	// Backward compatibility: check error message
	// This supports existing code that returns custom error strings
	errMsg := err.Error()
	return errMsg == "not found" ||
		strings.HasSuffix(errMsg, "not found") ||
		strings.HasPrefix(errMsg, "not found:")
}
