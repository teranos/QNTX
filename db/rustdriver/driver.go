// Package rustdriver implements database/sql/driver over Rust's single SQLite connection.
//
// Go's database/sql routes all SQL through Rust via CGO FFI, eliminating
// the dual-driver architecture (rusqlite + mattn/go-sqlite3) that caused
// SQLITE_CORRUPT errors.
//
// The driver shares the *C.SqliteStore pointer and sync.Mutex with the
// existing RustStore (ats/storage/sqlitecgo). All operations are serialized
// through the mutex since rusqlite::Connection is not thread-safe.
package rustdriver

/*
#cgo CFLAGS: -I${SRCDIR}/../../crates/qntx-sqlite/include
#cgo linux LDFLAGS: -L${SRCDIR}/../../target/release -lqntx_sqlite -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../target/release -lqntx_sqlite -lpthread -ldl -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../target/release -lqntx_sqlite -lws2_32 -luserenv

#include "storage_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"io"
	"sync"
	"time"
	"unsafe"

	"github.com/teranos/QNTX/errors"
)

// Register registers the "rustsqlite" driver with database/sql.
// storePtr is an unsafe.Pointer to *C.SqliteStore, and mu serializes access.
func Register(storePtr unsafe.Pointer, mu *sync.Mutex) {
	sql.Register("rustsqlite", &RustDriver{
		store: (*C.SqliteStore)(storePtr),
		mu:    mu,
	})
}

// RustDriver implements driver.Driver.
type RustDriver struct {
	store *C.SqliteStore
	mu    *sync.Mutex
}

func (d *RustDriver) Open(_ string) (driver.Conn, error) {
	return &RustConn{store: d.store, mu: d.mu}, nil
}

// RustConn implements driver.Conn.
type RustConn struct {
	store *C.SqliteStore
	mu    *sync.Mutex
}

func (c *RustConn) Prepare(query string) (driver.Stmt, error) {
	return &RustStmt{store: c.store, mu: c.mu, query: query}, nil
}

func (c *RustConn) Begin() (driver.Tx, error) {
	c.mu.Lock()
	result := C.sql_begin(c.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)
	c.mu.Unlock()

	if !success {
		return nil, errors.Newf("BEGIN IMMEDIATE failed: %s", errMsg)
	}
	return &RustTx{store: c.store, mu: c.mu}, nil
}

func (c *RustConn) Close() error {
	// Connection lifecycle is managed by RustStore — nothing to close here.
	return nil
}

// RustTx implements driver.Tx.
type RustTx struct {
	store *C.SqliteStore
	mu    *sync.Mutex
}

func (tx *RustTx) Commit() error {
	tx.mu.Lock()
	result := C.sql_commit(tx.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)
	tx.mu.Unlock()

	if !success {
		return errors.Newf("COMMIT failed: %s", errMsg)
	}
	return nil
}

func (tx *RustTx) Rollback() error {
	tx.mu.Lock()
	result := C.sql_rollback(tx.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)
	tx.mu.Unlock()

	if !success {
		return errors.Newf("ROLLBACK failed: %s", errMsg)
	}
	return nil
}

// RustStmt implements driver.Stmt.
type RustStmt struct {
	store *C.SqliteStore
	mu    *sync.Mutex
	query string
}

// NumInput returns -1 to indicate variable number of args.
func (s *RustStmt) NumInput() int {
	return -1
}

func (s *RustStmt) Close() error {
	return nil
}

func (s *RustStmt) Exec(args []driver.Value) (driver.Result, error) {
	paramsJSON, err := marshalParams(args)
	if err != nil {
		return nil, err
	}

	cSQL := C.CString(s.query)
	defer C.free(unsafe.Pointer(cSQL))
	cParams := C.CString(paramsJSON)
	defer C.free(unsafe.Pointer(cParams))

	s.mu.Lock()
	result := C.sql_exec(s.store, cSQL, cParams)
	success := bool(result.success)
	var errMsg string
	var lastID, affected int64
	if success {
		lastID = int64(result.last_insert_id)
		affected = int64(result.rows_affected)
	} else {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)
	s.mu.Unlock()

	if !success {
		return nil, errors.Newf("exec failed: %s", errMsg)
	}
	return &RustResult{lastInsertID: lastID, rowsAffected: affected}, nil
}

func (s *RustStmt) Query(args []driver.Value) (driver.Rows, error) {
	paramsJSON, err := marshalParams(args)
	if err != nil {
		return nil, err
	}

	cSQL := C.CString(s.query)
	defer C.free(unsafe.Pointer(cSQL))
	cParams := C.CString(paramsJSON)
	defer C.free(unsafe.Pointer(cParams))

	s.mu.Lock()
	result := C.sql_query(s.store, cSQL, cParams)
	var success bool
	var errMsg, colsJSON, rowsJSON string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	} else {
		if result.columns_json != nil {
			colsJSON = C.GoString(result.columns_json)
		}
		if result.rows_json != nil {
			rowsJSON = C.GoString(result.rows_json)
		}
	}
	C.query_result_free(result)
	s.mu.Unlock()

	if !success {
		return nil, errors.Newf("query failed: %s", errMsg)
	}

	var columns []string
	if colsJSON != "" {
		if err := json.Unmarshal([]byte(colsJSON), &columns); err != nil {
			return nil, errors.Wrapf(err, "failed to parse columns JSON")
		}
	}

	var rows [][]interface{}
	if rowsJSON != "" && rowsJSON != "[]" {
		if err := json.Unmarshal([]byte(rowsJSON), &rows); err != nil {
			return nil, errors.Wrapf(err, "failed to parse rows JSON")
		}
		// Decode $blob wrappers into []byte at parse time
		for i, row := range rows {
			for j, val := range row {
				if m, ok := val.(map[string]interface{}); ok {
					if b64, ok := m["$blob"].(string); ok {
						decoded, err := base64Decode(b64)
						if err != nil {
							return nil, errors.Wrapf(err, "failed to decode blob at row %d col %d", i, j)
						}
						rows[i][j] = decoded
					}
				}
			}
		}
	}

	return &RustRows{columns: columns, rows: rows, pos: 0}, nil
}

// RustRows implements driver.Rows with pre-fetched in-memory data.
type RustRows struct {
	columns []string
	rows    [][]interface{}
	pos     int
}

func (r *RustRows) Columns() []string {
	return r.columns
}

func (r *RustRows) Close() error {
	return nil
}

func (r *RustRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}

	row := r.rows[r.pos]
	r.pos++

	for i, val := range row {
		if i >= len(dest) {
			break
		}
		switch v := val.(type) {
		case float64:
			// JSON unmarshals numbers as float64 — convert back to int64 where possible
			if v == float64(int64(v)) {
				dest[i] = int64(v)
			} else {
				dest[i] = v
			}
		case string:
			// SQLite stores timestamps as text. The old CGO driver auto-converted
			// to time.Time; replicate that here so callers scanning into *time.Time work.
			if t, ok := tryParseTime(v); ok {
				dest[i] = t
			} else {
				dest[i] = v
			}
		case nil:
			dest[i] = nil
		default:
			// []byte (decoded blobs), bool, etc.
			dest[i] = v
		}
	}

	return nil
}

// RustResult implements driver.Result.
type RustResult struct {
	lastInsertID int64
	rowsAffected int64
}

func (r *RustResult) LastInsertId() (int64, error) {
	return r.lastInsertID, nil
}

func (r *RustResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// base64Decode decodes a standard base64 string to raw bytes.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// SQLite timestamp formats, ordered by likelihood.
// Covers: datetime('now'), strftime, and RFC3339 variants.
var timeFormats = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999+07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// tryParseTime attempts to parse a string as a timestamp.
// Returns the parsed time and true if successful.
func tryParseTime(s string) (time.Time, bool) {
	if len(s) < 10 {
		return time.Time{}, false
	}
	// Quick check: must start with a digit (year)
	if s[0] < '0' || s[0] > '9' {
		return time.Time{}, false
	}
	for _, format := range timeFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// marshalParams converts driver.Value slice to JSON array string.
// []byte values are wrapped as {"$blob": "base64"} so Rust can reconstruct
// them as raw blobs (needed for sqlite-vec FLOAT32_BLOB columns).
func marshalParams(args []driver.Value) (string, error) {
	if len(args) == 0 {
		return "[]", nil
	}
	converted := make([]interface{}, len(args))
	for i, v := range args {
		if b, ok := v.([]byte); ok {
			converted[i] = map[string]string{"$blob": base64.StdEncoding.EncodeToString(b)}
		} else {
			converted[i] = v
		}
	}
	b, err := json.Marshal(converted)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal query params")
	}
	return string(b), nil
}
