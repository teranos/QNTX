//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] Held is a DuckDB a subsystem holds for itself (crates/ats-duckdb/src/held.rs):」
// 「[Opus 5.5] its own tables, made by its own migrations, in one file of the node's.」
type Held struct {
	ptr  unsafe.Pointer // *C.HeldDb
	path string
	mu   sync.Mutex
}

// 「[Opus 5.5] HeldMigration is one of a subsystem's migrations: its version, and its SQL.」
type HeldMigration struct {
	Version string `json:"version"`
	SQL     string `json:"sql"`
}

// 「[Opus 5.5] OpenHeld opens the DuckDB at path, making it when there is none, and applies」
// 「[Opus 5.5] the migrations not yet applied, in order. One process holds a file once.」
func OpenHeld(path string, migrations []HeldMigration) (*Held, error) {
	body, err := json.Marshal(append(make([]HeldMigration, 0, len(migrations)), migrations...))
	if err != nil {
		return nil, errors.Wrapf(err, "the migrations of the DuckDB at %s did not encode", path)
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cMigrations := C.CString(string(body))
	defer C.free(unsafe.Pointer(cMigrations))

	var said *C.char
	ptr := C.duckdb_held_new(cPath, cMigrations, &said)
	if ptr == nil {
		return nil, reasonf(said, "failed to open the DuckDB at %s", path)
	}
	return &Held{ptr: unsafe.Pointer(ptr), path: path}, nil
}

// 「[Opus 5.5] Close releases the DuckDB.」
func (h *Held) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ptr != nil {
		C.duckdb_held_free((*C.HeldDb)(h.ptr))
		h.ptr = nil
	}
}

// 「[Opus 5.5] Exec runs SQL answering no rows, params bound in order, and answers how many」
// 「[Opus 5.5] rows it changed. With no params, sql may hold several statements.」
func (h *Held) Exec(sql string, params ...any) (int, error) {
	cSQL, cParams, err := h.statement(sql, params)
	if err != nil {
		return 0, err
	}
	defer C.free(unsafe.Pointer(cSQL))
	defer C.free(unsafe.Pointer(cParams))

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ptr == nil {
		return 0, errors.Newf("the DuckDB at %s is closed", h.path)
	}
	result := C.duckdb_held_exec((*C.HeldDb)(h.ptr), cSQL, cParams)
	defer C.duckdb_count_result_free(result)
	if !bool(result.success) {
		return 0, failed(result.error_msg, "the DuckDB at %s did not run %.200s", h.path, sql)
	}
	return int(result.count), nil
}

// 「[Opus 5.5] Query runs SQL, params bound in order, and decodes the rows it answered,」
// 「[Opus 5.5] a JSON array of objects by column name, into into.」
func (h *Held) Query(into any, sql string, params ...any) error {
	cSQL, cParams, err := h.statement(sql, params)
	if err != nil {
		return err
	}
	defer C.free(unsafe.Pointer(cSQL))
	defer C.free(unsafe.Pointer(cParams))

	h.mu.Lock()
	if h.ptr == nil {
		h.mu.Unlock()
		return errors.Newf("the DuckDB at %s is closed", h.path)
	}
	result := C.duckdb_held_query((*C.HeldDb)(h.ptr), cSQL, cParams)
	h.mu.Unlock()
	defer C.duckdb_held_result_free(result)
	if !bool(result.success) {
		return failed(result.error_msg, "the DuckDB at %s did not answer %.200s", h.path, sql)
	}
	rows := C.GoString(result.rows_json)
	if err := json.Unmarshal([]byte(rows), into); err != nil {
		return errors.Wrapf(err, "the rows the DuckDB at %s answered did not decode: %.200s", h.path, rows)
	}
	return nil
}

func (h *Held) statement(sql string, params []any) (*C.char, *C.char, error) {
	body, err := json.Marshal(append(make([]any, 0, len(params)), params...))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "the parameters of %.200s did not encode", sql)
	}
	return C.CString(sql), C.CString(string(body)), nil
}
