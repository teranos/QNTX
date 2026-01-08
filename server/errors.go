package server

import "github.com/teranos/QNTX/errors"

// Sentinel errors for common cases.
// Use these with errors.Is() for type-safe error checking.
// Wrap these with errors.Wrap() to add context while preserving the type.
var (
	// ErrNotFound indicates the requested resource does not exist
	ErrNotFound = errors.New("not found")

	// ErrInvalidRequest indicates the request was malformed or invalid
	ErrInvalidRequest = errors.New("invalid request")

	// ErrUnauthorized indicates the request lacks proper authentication
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden indicates the request is not allowed for this user
	ErrForbidden = errors.New("forbidden")

	// ErrServiceUnavailable indicates a required service is not available
	ErrServiceUnavailable = errors.New("service unavailable")

	// ErrTimeout indicates an operation timed out
	ErrTimeout = errors.New("operation timed out")

	// ErrConflict indicates a resource conflict (e.g., duplicate key)
	ErrConflict = errors.New("resource conflict")
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
	return len(errMsg) >= 9 && (errMsg == "not found" ||
		errMsg[len(errMsg)-9:] == "not found" ||
		len(errMsg) > 10 && errMsg[:10] == "not found:")
}

// IsInvalidRequestError checks if an error is or wraps ErrInvalidRequest
func IsInvalidRequestError(err error) bool {
	return err != nil && errors.Is(err, ErrInvalidRequest)
}

// IsServiceUnavailableError checks if an error is or wraps ErrServiceUnavailable
func IsServiceUnavailableError(err error) bool {
	return err != nil && errors.Is(err, ErrServiceUnavailable)
}

// WrapNotFound wraps an error as a not-found error with context
func WrapNotFound(err error, context string) error {
	return errors.Wrap(errors.Wrap(ErrNotFound, err.Error()), context)
}

// WrapInvalidRequest wraps an error as an invalid-request error with context
func WrapInvalidRequest(err error, context string) error {
	return errors.Wrap(errors.Wrap(ErrInvalidRequest, err.Error()), context)
}

// NewNotFoundError creates a not-found error with a formatted message
func NewNotFoundError(format string, args ...interface{}) error {
	return errors.Wrap(ErrNotFound, errors.Newf(format, args...).Error())
}

// NewInvalidRequestError creates an invalid-request error with a formatted message
func NewInvalidRequestError(format string, args ...interface{}) error {
	return errors.Wrap(ErrInvalidRequest, errors.Newf(format, args...).Error())
}
