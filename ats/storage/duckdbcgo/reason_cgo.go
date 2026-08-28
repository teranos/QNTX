//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import "github.com/teranos/errors"

// Why the other side said no.

// A constructor answers with a pointer, so a failure arrives as NULL and the
// reason rides an out-parameter. Before that it went to the crate's stderr and
// Go wrote its own summary of a cause it never received.

// reasonf turns what Rust wrote into a Go error and frees it. A null message
// against a null store is the crate failing without saying why, which is a bug
// there rather than a store that is merely unreachable — and the error says so
// instead of inventing one.
func reasonf(said *C.char, format string, args ...any) error {
	if said == nil {
		return errors.Newf(format+", and the reason was not recorded", args...)
	}
	defer C.duckdb_string_free(said)
	return errors.Newf(format+": %s", append(args, C.GoString(said))...)
}

// took reports a message the callee handed back outside a failure — a flush
// that could not be written while the store was closing. Nil when there was
// nothing to say.
func took(said *C.char) error {
	if said == nil {
		return nil
	}
	defer C.duckdb_string_free(said)
	return errors.New(C.GoString(said))
}
