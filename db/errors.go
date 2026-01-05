package db

import (
	"strings"

	"github.com/teranos/QNTX/errors"
)

// ErrDatabaseClosed is returned when operations are attempted on a closed database.
// This typically occurs during graceful shutdown when the database connection
// is closed before all goroutines have finished their work.
var ErrDatabaseClosed = errors.New("database is closed")

// IsDatabaseClosed checks if an error indicates the database connection is closed.
// This handles both:
// - Wrapped ErrDatabaseClosed errors from this package
// - Raw SQLite/sql driver errors that contain "database is closed" in their message
//
// The string matching fallback is necessary because the underlying sql driver
// returns its own error types that we cannot wrap at the source.
func IsDatabaseClosed(err error) bool {
	if err == nil {
		return false
	}

	// Check for our wrapped error type first (preferred)
	if errors.Is(err, ErrDatabaseClosed) {
		return true
	}

	// Fallback: check for raw driver error messages
	// This handles cases where errors come directly from sql/sqlite driver
	errMsg := err.Error()
	return strings.Contains(errMsg, "database is closed") ||
		strings.Contains(errMsg, "sql: database is closed")
}
