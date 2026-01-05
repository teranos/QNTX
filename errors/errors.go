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

// Common sentinel errors can be defined like:
//   var ErrNotFound = errors.New("not found")
//   var ErrClosed = errors.New("closed")
