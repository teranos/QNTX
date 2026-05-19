// Package rustdriver implements database/sql/driver over Rust's SQLite connections.
//
// Go's database/sql routes all SQL through Rust via CGO FFI, eliminating
// the dual-driver architecture (rusqlite + mattn/go-sqlite3) that caused
// SQLITE_CORRUPT errors.
//
// The Rust store holds two connections: one for writes, one for reads (WAL mode).
// The driver uses separate mutexes (muWrite for Exec/Begin/Commit/Rollback,
// muRead for Query) so reads never block writes and vice versa.
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

	"github.com/teranos/errors"
)

// Register registers the "rustsqlite" driver with database/sql.
// storePtr is the write store, readConnPtr is the read-only connection (may be nil).
// muWrite serializes write operations, muRead serializes read operations.
func Register(storePtr, readConnPtr unsafe.Pointer, muWrite, muRead *sync.Mutex) {
	sql.Register("rustsqlite", &RustDriver{
		store:    (*C.SqliteStore)(storePtr),
		readConn: (*C.ReadConn)(readConnPtr),
		muWrite:  muWrite,
		muRead:   muRead,
	})
}

// RegisterNamed registers an additional named driver that tags all operations
// with the given caller in the flight recorder. Use for components that open
// their own sql.DB (db stats, watcher handlers, etc).
func RegisterNamed(name, caller string, storePtr, readConnPtr unsafe.Pointer, muWrite, muRead *sync.Mutex) {
	sql.Register(name, &RustDriver{
		store:    (*C.SqliteStore)(storePtr),
		readConn: (*C.ReadConn)(readConnPtr),
		muWrite:  muWrite,
		muRead:   muRead,
		caller:   caller,
	})
}

// SetCaller sets the flight recorder caller tag for the current OS thread.
// Call this before issuing queries to attribute them to their source component.
// The tag persists on the thread until changed.
func SetCaller(caller string) {
	setCaller(caller)
}

func setCaller(caller string) {
	cCaller := C.CString(caller)
	defer C.free(unsafe.Pointer(cCaller))
	C.flight_recorder_set_caller(cCaller)
}

// RustDriver implements driver.Driver.
type RustDriver struct {
	store    *C.SqliteStore
	readConn *C.ReadConn
	muWrite  *sync.Mutex
	muRead   *sync.Mutex
	caller   string // flight recorder caller tag (empty = unattributed)
}

func (d *RustDriver) Open(_ string) (driver.Conn, error) {
	return &RustConn{store: d.store, readConn: d.readConn, muWrite: d.muWrite, muRead: d.muRead, caller: d.caller}, nil
}

// RustConn implements driver.Conn.
type RustConn struct {
	store    *C.SqliteStore
	readConn *C.ReadConn
	muWrite  *sync.Mutex
	muRead   *sync.Mutex
	inTx     bool   // true while Begin() holds muWrite — Exec/Query skip write lock
	caller   string // flight recorder caller tag
}

func (c *RustConn) Prepare(query string) (driver.Stmt, error) {
	return &RustStmt{store: c.store, readConn: c.readConn, muWrite: c.muWrite, muRead: c.muRead, conn: c, query: query, caller: c.caller}, nil
}

func (c *RustConn) Begin() (driver.Tx, error) {
	// Hold muWrite for the entire transaction lifetime — released by Commit/Rollback.
	// All connections share one rusqlite::Connection, so only one transaction
	// can be active at a time. Without holding across the lifetime, a second
	// goroutine's Begin() would succeed between Lock/Unlock but hit
	// "cannot start a transaction within a transaction" on the shared connection.
	c.muWrite.Lock()
	result := C.sql_begin(c.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)

	if !success {
		c.muWrite.Unlock()
		return nil, errors.Newf("BEGIN IMMEDIATE failed: %s", errMsg)
	}
	// muWrite stays locked — Commit/Rollback will unlock it.
	c.inTx = true
	return &RustTx{store: c.store, muWrite: c.muWrite, conn: c}, nil
}

func (c *RustConn) Close() error {
	// Connection lifecycle is managed by RustStore — nothing to close here.
	return nil
}

// RustTx implements driver.Tx.
type RustTx struct {
	store   *C.SqliteStore
	muWrite *sync.Mutex
	conn    *RustConn
}

func (tx *RustTx) Commit() error {
	// muWrite is held since Begin() — release it after COMMIT.
	defer tx.muWrite.Unlock()
	tx.conn.inTx = false
	result := C.sql_commit(tx.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)

	if !success {
		return errors.Newf("COMMIT failed: %s", errMsg)
	}
	return nil
}

func (tx *RustTx) Rollback() error {
	// muWrite is held since Begin() — release it after ROLLBACK.
	defer tx.muWrite.Unlock()
	tx.conn.inTx = false
	result := C.sql_rollback(tx.store)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.exec_result_free(result)

	if !success {
		return errors.Newf("ROLLBACK failed: %s", errMsg)
	}
	return nil
}

// RustStmt implements driver.Stmt.
type RustStmt struct {
	store    *C.SqliteStore
	readConn *C.ReadConn
	muWrite  *sync.Mutex
	muRead   *sync.Mutex
	conn     *RustConn // back-pointer to check inTx
	query    string
	caller   string // flight recorder caller tag
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

	// If inside a transaction, muWrite is already held by Begin() — don't re-lock.
	if !s.conn.inTx {
		s.muWrite.Lock()
		defer s.muWrite.Unlock()
	}
	if s.caller != "" {
		setCaller(s.caller)
	}
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

	// Lock appropriately: readConn uses muRead, write conn uses muWrite.
	// Inside a transaction, muWrite is already held — skip locking.
	if s.readConn != nil {
		s.muRead.Lock()
		defer s.muRead.Unlock()
	} else if !s.conn.inTx {
		s.muWrite.Lock()
		defer s.muWrite.Unlock()
	}
	if s.caller != "" {
		setCaller(s.caller)
	}
	var result C.QueryResultC
	if s.readConn != nil {
		result = C.read_conn_sql_query(s.readConn, cSQL, cParams)
	} else {
		result = C.sql_query(s.store, cSQL, cParams)
	}
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
