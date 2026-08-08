//go:build cgo && rustduckdb

package duckdbcgo

import (
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// The crate has its own tests; these prove the boundary between them — a
// declaration written from Go comes back from Go, and progress folded in Rust
// is readable through the same handle.

func newScheduleStore(t *testing.T, location string) *ScheduleStore {
	t.Helper()
	store, err := NewScheduleStore("file://" + location)
	if err != nil {
		t.Fatalf("NewScheduleStore: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func scheduleDeclaration(id string) *protocol.ScheduleDeclaration {
	return &protocol.ScheduleDeclaration{
		Id:              id,
		HandlerName:     "plugin.handler",
		IntervalSeconds: 600,
		State:           "active",
		Metadata:        `{"plugin":"plugin"}`,
		CreatedAtMs:     1_700_000_000_000,
		FirstRunAtMs:    1_700_000_000_000,
	}
}

func TestScheduleDeclarationCrossesTheBoundary(t *testing.T) {
	store := newScheduleStore(t, t.TempDir())

	if err := store.Put(scheduleDeclaration("s1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one declaration, got %d", len(listed))
	}
	if listed[0].HandlerName != "plugin.handler" {
		t.Errorf("handler crossed as %q", listed[0].HandlerName)
	}
	if listed[0].IntervalSeconds != 600 {
		t.Errorf("interval crossed as %d", listed[0].IntervalSeconds)
	}
}

func TestScheduleProgressIsFoldedFromTicks(t *testing.T) {
	store := newScheduleStore(t, t.TempDir())

	if err := store.Put(scheduleDeclaration("s1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.RecordRun("s1", 1_700_000_005_000, "JB-1", 1_700_000_605_000); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	progress, err := store.Progress("s1")
	if err != nil {
		t.Fatalf("Progress: %v", err)
	}
	if progress.RunCount != 1 {
		t.Errorf("run count crossed as %d", progress.RunCount)
	}
	if progress.LastExecutionId != "JB-1" {
		t.Errorf("last execution crossed as %q", progress.LastExecutionId)
	}
	if progress.NextRunAtMs != 1_700_000_605_000 {
		t.Errorf("next run crossed as %d", progress.NextRunAtMs)
	}
}

func TestOnlyDueSchedulesCrossBack(t *testing.T) {
	store := newScheduleStore(t, t.TempDir())

	owed := scheduleDeclaration("owed")
	if err := store.Put(owed); err != nil {
		t.Fatalf("Put owed: %v", err)
	}
	later := scheduleDeclaration("later")
	later.FirstRunAtMs = 1_900_000_000_000
	if err := store.Put(later); err != nil {
		t.Fatalf("Put later: %v", err)
	}

	due, err := store.Due(1_700_000_000_000)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 || due[0].Id != "owed" {
		t.Fatalf("expected only the owed schedule, got %v", due)
	}
}

func TestWithdrawnScheduleKeepsItsTicks(t *testing.T) {
	dir := t.TempDir()

	store := newScheduleStore(t, dir)
	if err := store.Put(scheduleDeclaration("s1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.RecordRun("s1", 1_700_000_005_000, "JB-1", 1_700_000_605_000); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := store.Delete("s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	store.Close()

	reopened := newScheduleStore(t, dir)
	listed, err := reopened.List()
	if err != nil {
		t.Fatalf("List after reopen: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("the withdrawn declaration came back: %v", listed)
	}

	progress, err := reopened.Progress("s1")
	if err != nil {
		t.Fatalf("Progress after reopen: %v", err)
	}
	if progress.RunCount != 1 {
		t.Errorf("what ran should still have run, got %d", progress.RunCount)
	}
}

func TestWithdrawingNothingIsAnError(t *testing.T) {
	store := newScheduleStore(t, t.TempDir())

	if err := store.Delete("never-declared"); err == nil {
		t.Fatal("a delete that hit nothing reported success")
	}
}
