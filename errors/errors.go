// Package errors provides error handling for QNTX.
//
// This package re-exports github.com/cockroachdb/errors, providing:
//   - Stack traces for debugging
//   - Error wrapping and context
//   - PII-safe error formatting
//   - Network portability for distributed systems
//   - Sentry integration
//
// Usage:
//
//	// Create new error
//	err := errors.New("something went wrong")
//
//	// Wrap with context
//	if err := doSomething(); err != nil {
//	    return errors.Wrap(err, "failed to do something")
//	}
//
//	// Add hints for users
//	return errors.WithHint(err, "try increasing the timeout")
//
//	// Check errors
//	if errors.Is(err, sql.ErrNoRows) {
//	    // handle not found
//	}
//
// For full documentation see: https://pkg.go.dev/github.com/cockroachdb/errors
package errors

import (
	crdb "github.com/cockroachdb/errors"
)

// Core error creation and wrapping
var (
	New          = crdb.New
	Newf         = crdb.Newf
	Wrap         = crdb.Wrap
	Wrapf        = crdb.Wrapf
	WithStack    = crdb.WithStack
	WithMessage  = crdb.WithMessage
	WithMessagef = crdb.WithMessagef
)

// User-facing messages and details
var (
	WithHint          = crdb.WithHint
	WithHintf         = crdb.WithHintf
	WithDetail        = crdb.WithDetail
	WithDetailf       = crdb.WithDetailf
	WithSafeDetails   = crdb.WithSafeDetails
	WithSecondaryError = crdb.WithSecondaryError
)

// Error inspection
var (
	Is        = crdb.Is
	IsAny     = crdb.IsAny
	As        = crdb.As
	Unwrap    = crdb.Unwrap
	UnwrapOnce = crdb.UnwrapOnce
	UnwrapAll = crdb.UnwrapAll
	GetAllHints = crdb.GetAllHints
	GetAllDetails = crdb.GetAllDetails
	FlattenHints = crdb.FlattenHints
	FlattenDetails = crdb.FlattenDetails
)

// Advanced features
var (
	Handled            = crdb.Handled
	HandledWithMessage = crdb.HandledWithMessage
	WithDomain         = crdb.WithDomain
	GetDomain          = crdb.GetDomain
	WithContextTags    = crdb.WithContextTags
	EncodeError        = crdb.EncodeError
	DecodeError        = crdb.DecodeError
	GetReportableStackTrace = crdb.GetReportableStackTrace
)

// GetStack is an alias for GetReportableStackTrace for convenience.
var GetStack = crdb.GetReportableStackTrace

// Assertions and panics
var (
	AssertionFailedf  = crdb.AssertionFailedf
	NewAssertionErrorWithWrappedErrf = crdb.NewAssertionErrorWithWrappedErrf
)

// Common sentinel errors for use across QNTX.
// Use these with errors.Is() for type-safe error checking.
// Wrap these with errors.Wrap() to add context while preserving the type.
var (
	// ErrNotFound indicates the requested resource does not exist
	ErrNotFound = New("not found")

	// ErrInvalidRequest indicates the request was malformed or invalid
	ErrInvalidRequest = New("invalid request")

	// ErrUnauthorized indicates the request lacks proper authentication
	ErrUnauthorized = New("unauthorized")

	// ErrForbidden indicates the request is not allowed for this user
	ErrForbidden = New("forbidden")

	// ErrServiceUnavailable indicates a required service is not available
	ErrServiceUnavailable = New("service unavailable")

	// ErrTimeout indicates an operation timed out
	ErrTimeout = New("operation timed out")

	// ErrConflict indicates a resource conflict (e.g., duplicate key)
	ErrConflict = New("resource conflict")
)

// IsNotFoundError checks if an error is or wraps ErrNotFound.
// Also provides backward compatibility with string-based "not found" errors.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check if error is or wraps our sentinel error
	if Is(err, ErrNotFound) {
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
	return err != nil && Is(err, ErrInvalidRequest)
}

// IsServiceUnavailableError checks if an error is or wraps ErrServiceUnavailable
func IsServiceUnavailableError(err error) bool {
	return err != nil && Is(err, ErrServiceUnavailable)
}

// WrapNotFound wraps an error as a not-found error with context
func WrapNotFound(err error, context string) error {
	return Wrap(Wrap(ErrNotFound, err.Error()), context)
}

// WrapInvalidRequest wraps an error as an invalid-request error with context
func WrapInvalidRequest(err error, context string) error {
	return Wrap(Wrap(ErrInvalidRequest, err.Error()), context)
}

// NewNotFoundError creates a not-found error with a formatted message
func NewNotFoundError(format string, args ...interface{}) error {
	return Wrap(ErrNotFound, Newf(format, args...).Error())
}

// NewInvalidRequestError creates an invalid-request error with a formatted message
func NewInvalidRequestError(format string, args ...interface{}) error {
	return Wrap(ErrInvalidRequest, Newf(format, args...).Error())
}
