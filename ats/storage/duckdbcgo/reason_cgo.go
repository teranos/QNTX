//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/errors"
)

// What crossed the FFI, as the value it is.

// The crate writes the sacred shape into every error slot. It is decoded here
// and nowhere else, so the one place that reads C memory is the one place
// that turns bytes back into a typed error. What did not decode is kept whole
// as Unshaped rather than replaced with a guess.

// crossed reads and frees an error slot. Nil in is nil out: a slot the crate
// did not write is no error.
func crossed(said *C.char) error {
	if said == nil {
		return nil
	}
	defer C.duckdb_string_free(said)
	return sacred.Decode(C.GoString(said))
}

// reasonf is crossed with context for a constructor that answered NULL. A
// null message against a null store is the crate failing without saying why,
// which is a bug there rather than a store that is merely unreachable — and
// the error says so instead of inventing one.
func reasonf(said *C.char, format string, args ...any) error {
	if said == nil {
		return errors.Newf(format+", and the reason was not recorded", args...)
	}
	return errors.Wrapf(crossed(said), format, args...)
}

// took reports a message the callee handed back outside a failure — a flush
// that could not be written while the store was closing. Nil when there was
// nothing to say.
func took(said *C.char) error {
	return crossed(said)
}

// failed is crossed with context for a result struct that said no. The slot
// is read here; the caller still frees the struct.
func failed(said *C.char, format string, args ...any) error {
	if said == nil {
		return errors.Newf(format+", and the reason was not recorded", args...)
	}
	return errors.Wrapf(sacred.Decode(C.GoString(said)), format, args...)
}
