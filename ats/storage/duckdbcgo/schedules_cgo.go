//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/qntx-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// ScheduleStore is the parquet-backend schedule store, held by the Rust crate:
// a declaration is an object under `<location>/schedules/`, a tick is a row
// under `<location>/schedule_ticks/` that Flush turns into a file (ADR-028).
type ScheduleStore struct {
	ptr unsafe.Pointer // *C.ScheduleStore
	mu  sync.Mutex
}

// NewScheduleStore opens the schedule store at a storage location URL.
func NewScheduleStore(location string) (*ScheduleStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))

	ptr := C.duckdb_schedules_new(cLocation)
	if ptr == nil {
		return nil, errors.Newf("failed to open the schedule store at %s", location)
	}
	return &ScheduleStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close flushes buffered ticks, then releases the Rust-owned store.
func (s *ScheduleStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_schedules_free((*C.ScheduleStore)(s.ptr))
		s.ptr = nil
	}
}

// Put declares a schedule, replacing any under the same id. Returns when the
// object is durable.
func (s *ScheduleStore) Put(declaration *protocol.ScheduleDeclaration) error {
	body, err := json.Marshal(declaration)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize schedule %s", declaration.Id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cBody := C.CString(string(body))
	defer C.free(unsafe.Pointer(cBody))

	result := C.duckdb_schedules_put((*C.ScheduleStore)(s.ptr), cBody)
	return storageResultErr(result, "declare schedule "+declaration.Id)
}

// List returns every declaration, ordered by creation.
func (s *ScheduleStore) List() ([]*protocol.ScheduleDeclaration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_schedules_list((*C.ScheduleStore)(s.ptr))
	return decodeDeclarations(result, "list schedules")
}

// Due returns the active declarations owed at atMillis.
func (s *ScheduleStore) Due(atMillis int64) ([]*protocol.ScheduleDeclaration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_schedules_due((*C.ScheduleStore)(s.ptr), C.int64_t(atMillis))
	return decodeDeclarations(result, "list due schedules")
}

// Next returns the soonest run owed, or nil when nothing is scheduled.
func (s *ScheduleStore) Next() (*protocol.ScheduleDeclaration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_schedules_next((*C.ScheduleStore)(s.ptr))
	declarations, err := decodeDeclarations(result, "read the next schedule")
	if err != nil || len(declarations) == 0 {
		return nil, err
	}
	return declarations[0], nil
}

// Delete withdraws a declaration. The ticks it emitted stay.
func (s *ScheduleStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_schedules_delete((*C.ScheduleStore)(s.ptr), cID)
	return storageResultErr(result, "withdraw schedule "+id)
}

// RecordRun notes a run. Buffered — this is the call on the tick path and it
// does not reach storage.
func (s *ScheduleStore) RecordRun(id string, atMillis int64, executionID string, nextRunAtMillis int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))
	cExecution := C.CString(executionID)
	defer C.free(unsafe.Pointer(cExecution))

	result := C.duckdb_schedules_record_run(
		(*C.ScheduleStore)(s.ptr), cID, C.int64_t(atMillis), cExecution, C.int64_t(nextRunAtMillis))
	return storageResultErr(result, "record a run for schedule "+id)
}

// Reschedule moves the next run without a run having happened.
func (s *ScheduleStore) Reschedule(id string, atMillis, nextRunAtMillis int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_schedules_reschedule(
		(*C.ScheduleStore)(s.ptr), cID, C.int64_t(atMillis), C.int64_t(nextRunAtMillis))
	return storageResultErr(result, "reschedule "+id)
}

// Flush writes the buffered ticks as one file.
func (s *ScheduleStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_schedules_flush((*C.ScheduleStore)(s.ptr))
	return storageResultErr(result, "flush schedule ticks")
}

// Progress returns what the ticks derive for one schedule. One that never ran
// has zeroes rather than an error.
func (s *ScheduleStore) Progress(id string) (*protocol.ScheduleProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_schedules_progress((*C.ScheduleStore)(s.ptr), cID)
	body, err := schedulesResultBody(result, "read the progress for schedule "+id)
	if err != nil {
		return nil, err
	}

	var progress protocol.ScheduleProgress
	if err := json.Unmarshal([]byte(body), &progress); err != nil {
		return nil, errors.Wrapf(err, "failed to parse the progress for schedule %s", id)
	}
	return &progress, nil
}

// decodeDeclarations reads a JSON array of declarations out of a result.
func decodeDeclarations(result C.SchedulesResultC, operation string) ([]*protocol.ScheduleDeclaration, error) {
	body, err := schedulesResultBody(result, operation)
	if err != nil {
		return nil, err
	}

	var declarations []*protocol.ScheduleDeclaration
	if err := json.Unmarshal([]byte(body), &declarations); err != nil {
		return nil, errors.Wrapf(err, "failed to parse the result of %s", operation)
	}
	return declarations, nil
}

// schedulesResultBody frees the result and returns its JSON, or the failure it
// carried.
func schedulesResultBody(result C.SchedulesResultC, operation string) (string, error) {
	defer C.duckdb_schedules_result_free(result)
	if !bool(result.success) {
		message := C.GoString(result.error_msg)
		if message == "" {
			message = "the parquet backend reported failure without a message"
		}
		return "", errors.Newf("failed to %s: %s", operation, message)
	}
	return C.GoString(result.schedules_json), nil
}
