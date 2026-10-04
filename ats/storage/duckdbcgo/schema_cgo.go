//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
*/
import "C"

import "github.com/teranos/errors"

// SchemaTables is the tables ats-duckdb's migrations leave standing, applied
// by its own runner in the DuckDB it links.
func SchemaTables() ([]string, error) {
	result := C.duckdb_schema_tables()
	defer C.duckdb_schema_result_free(result)

	if !bool(result.success) {
		return nil, failed(result.error_msg, "failed to read the tables ats-duckdb's migrations leave")
	}
	var tables []string
	if err := readBack([]byte(C.GoString(result.tables_json)), &tables); err != nil {
		return nil, errors.Wrap(err, "failed to parse the tables ats-duckdb's migrations leave")
	}
	return tables, nil
}
