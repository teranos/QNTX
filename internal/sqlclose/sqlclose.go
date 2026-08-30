// Package sqlclose folds a deferred Close failure into the function's
// returned error, so a handle that did not close cleanly is part of the
// answer rather than a value nothing read.
package sqlclose

import (
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// Log reports a Close failure — for functions with no error return, where
// the log is the only channel left:
// `defer func() { sqlclose.Log(rows.Close(), logger, "the rows") }()`.
func Log(cerr error, logger *zap.SugaredLogger, what string) {
	if cerr != nil && logger != nil {
		logger.Warnw("Close failed", "what", what, "error", cerr)
	}
}

// With folds a Close result into err at a deferred call site that names
// rows.Close() itself, which static close-checkers can see:
// `defer func() { err = sqlclose.With(err, rows.Close(), "the rows") }()`.
func With(err, cerr error, what string) error {
	switch {
	case cerr == nil:
		return err
	case err == nil:
		return errors.Wrapf(cerr, "failed to close %s", what)
	default:
		return errors.WithSecondaryError(err, errors.Wrapf(cerr, "failed to close %s", what))
	}
}
