package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestWatcherStore_Create(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)

	watcher := &storage.Watcher{
		ID:   "test-watcher-1",
		Name: "Test Watcher",
		Filter: types.AxFilter{
			Subjects:   []string{"user:123"},
			Predicates: []string{"logged_in"},
		},
		ActionType:        storage.ActionTypePython,
		ActionData:        "print('hello')",
		MaxFiresPerMinute: 105,
		Enabled:           true,
	}

	ctx := context.Background()
	err := store.Create(ctx, watcher)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify creation time was set
	if watcher.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
	if watcher.UpdatedAt.IsZero() {
		t.Error("UpdatedAt was not set")
	}

	// Verify we can retrieve it
	retrieved, err := store.Get(ctx, watcher.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != watcher.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, watcher.ID)
	}
	if retrieved.Name != watcher.Name {
		t.Errorf("Name mismatch: got %s, want %s", retrieved.Name, watcher.Name)
	}
	if len(retrieved.Filter.Subjects) != 1 || retrieved.Filter.Subjects[0] != "user:123" {
		t.Errorf("Subjects mismatch: got %v, want [user:123]", retrieved.Filter.Subjects)
	}
}

func TestWatcherStore_CreateDuplicate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)

	watcher := &storage.Watcher{
		ID:         "duplicate-test",
		Name:       "Test",
		ActionType: storage.ActionTypePython,
		ActionData: "pass",
		Enabled:    true,
	}

	ctx := context.Background()
	err := store.Create(ctx, watcher)
	if err != nil {
		t.Fatalf("First Create failed: %v", err)
	}

	// Try to create again with same ID
	err = store.Create(ctx, watcher)
	if err == nil {
		t.Error("Expected error creating duplicate watcher, got nil")
	}
}

func TestWatcherStore_Get(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)

	ctx := context.Background()

	// Test getting non-existent watcher
	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error getting non-existent watcher, got nil")
	}

	// Create and retrieve
	watcher := &storage.Watcher{
		ID:         "get-test",
		Name:       "Get Test",
		ActionType: storage.ActionTypeWebhook,
		ActionData: "https://example.com/webhook",
		Enabled:    false,
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "get-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Enabled != false {
		t.Error("Expected Enabled=false")
	}
	if retrieved.ActionType != storage.ActionTypeWebhook {
		t.Errorf("ActionType mismatch: got %s, want %s", retrieved.ActionType, storage.ActionTypeWebhook)
	}
}

func TestWatcherStore_Update(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	// Create initial watcher
	watcher := &storage.Watcher{
		ID:         "update-test",
		Name:       "Original Name",
		ActionType: storage.ActionTypePython,
		ActionData: "print('original')",
		Enabled:    true,
		Filter: types.AxFilter{
			Subjects: []string{"original"},
		},
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	originalUpdatedAt := watcher.UpdatedAt

	// Wait a moment to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// Update it
	watcher.Name = "Updated Name"
	watcher.ActionData = "print('updated')"
	watcher.Filter.Subjects = []string{"updated"}
	watcher.Enabled = false

	if err := store.Update(ctx, watcher); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify UpdatedAt changed
	if !watcher.UpdatedAt.After(originalUpdatedAt) {
		t.Error("UpdatedAt was not updated")
	}

	// Retrieve and verify changes
	retrieved, err := store.Get(ctx, "update-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "Updated Name" {
		t.Errorf("Name not updated: got %s, want Updated Name", retrieved.Name)
	}
	if retrieved.ActionData != "print('updated')" {
		t.Errorf("ActionData not updated: got %s", retrieved.ActionData)
	}
	if retrieved.Enabled != false {
		t.Error("Enabled not updated")
	}
	if len(retrieved.Filter.Subjects) != 1 || retrieved.Filter.Subjects[0] != "updated" {
		t.Errorf("Subjects not updated: got %v", retrieved.Filter.Subjects)
	}
}

func TestWatcherStore_Delete(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	watcher := &storage.Watcher{
		ID:         "delete-test",
		Name:       "To Delete",
		ActionType: storage.ActionTypePython,
		ActionData: "pass",
		Enabled:    true,
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it exists
	_, err := store.Get(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Get failed before delete: %v", err)
	}

	// Delete it
	if err := store.Delete(ctx, "delete-test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = store.Get(ctx, "delete-test")
	if err == nil {
		t.Error("Expected error getting deleted watcher, got nil")
	}

	// Delete non-existent should not error
	err = store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Delete non-existent returned error: %v", err)
	}
}

func TestWatcherStore_List(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	// Create mix of enabled/disabled watchers
	watchers := []*storage.Watcher{
		{
			ID:         "list-test-1",
			Name:       "Enabled 1",
			ActionType: storage.ActionTypePython,
			ActionData: "pass",
			Enabled:    true,
		},
		{
			ID:         "list-test-2",
			Name:       "Disabled",
			ActionType: storage.ActionTypePython,
			ActionData: "pass",
			Enabled:    false,
		},
		{
			ID:         "list-test-3",
			Name:       "Enabled 2",
			ActionType: storage.ActionTypeWebhook,
			ActionData: "https://example.com",
			Enabled:    true,
		},
	}

	for _, w := range watchers {
		if err := store.Create(ctx, w); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all watchers
	all, err := store.List(ctx, false)
	if err != nil {
		t.Fatalf("List(false) failed: %v", err)
	}

	if len(all) != 3 {
		t.Errorf("List(false) returned %d watchers, want 3", len(all))
	}

	// List only enabled
	enabled, err := store.List(ctx, true)
	if err != nil {
		t.Fatalf("List(true) failed: %v", err)
	}

	if len(enabled) != 2 {
		t.Errorf("List(true) returned %d watchers, want 2", len(enabled))
	}

	// Verify all returned watchers are enabled
	for _, w := range enabled {
		if !w.Enabled {
			t.Errorf("List(true) returned disabled watcher: %s", w.ID)
		}
	}
}

func TestWatcherStore_TimeFilters(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	now := time.Now()
	future := now.Add(24 * time.Hour)

	watcher := &storage.Watcher{
		ID:         "time-test",
		Name:       "Time Filter Test",
		ActionType: storage.ActionTypePython,
		ActionData: "pass",
		Enabled:    true,
		Filter: types.AxFilter{
			TimeStart: &now,
			TimeEnd:   &future,
		},
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "time-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Filter.TimeStart == nil {
		t.Error("TimeStart was not persisted")
	} else if !retrieved.Filter.TimeStart.Equal(now) {
		t.Errorf("TimeStart mismatch: got %v, want %v", retrieved.Filter.TimeStart, now)
	}

	if retrieved.Filter.TimeEnd == nil {
		t.Error("TimeEnd was not persisted")
	} else if !retrieved.Filter.TimeEnd.Equal(future) {
		t.Errorf("TimeEnd mismatch: got %v, want %v", retrieved.Filter.TimeEnd, future)
	}
}

func TestWatcherStore_ComplexFilter(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	watcher := &storage.Watcher{
		ID:         "complex-filter",
		Name:       "Complex Filter Test",
		ActionType: storage.ActionTypePython,
		ActionData: "pass",
		Enabled:    true,
		Filter: types.AxFilter{
			Subjects:   []string{"user:123", "user:456", "user:789"},
			Predicates: []string{"logged_in", "logged_out"},
			Contexts:   []string{"web", "mobile"},
			Actors:     []string{"system", "admin"},
		},
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "complex-filter")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Verify all filter arrays
	if len(retrieved.Filter.Subjects) != 3 {
		t.Errorf("Subjects length: got %d, want 3", len(retrieved.Filter.Subjects))
	}
	if len(retrieved.Filter.Predicates) != 2 {
		t.Errorf("Predicates length: got %d, want 2", len(retrieved.Filter.Predicates))
	}
	if len(retrieved.Filter.Contexts) != 2 {
		t.Errorf("Contexts length: got %d, want 2", len(retrieved.Filter.Contexts))
	}
	if len(retrieved.Filter.Actors) != 2 {
		t.Errorf("Actors length: got %d, want 2", len(retrieved.Filter.Actors))
	}
}

func TestWatcherStore_UpdateStats(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewWatcherStore(db)
	ctx := context.Background()

	watcher := &storage.Watcher{
		ID:         "stats-test",
		Name:       "Stats Test",
		ActionType: storage.ActionTypePython,
		ActionData: "pass",
		Enabled:    true,
	}

	if err := store.Create(ctx, watcher); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Initial stats should be zero
	if watcher.FireCount != 0 {
		t.Errorf("Initial FireCount: got %d, want 0", watcher.FireCount)
	}
	if watcher.ErrorCount != 0 {
		t.Errorf("Initial ErrorCount: got %d, want 0", watcher.ErrorCount)
	}

	// Update stats
	now := time.Now()
	watcher.FireCount = 42
	watcher.ErrorCount = 3
	watcher.LastFiredAt = &now
	watcher.LastError = "test error"

	if err := store.Update(ctx, watcher); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify stats persisted
	retrieved, err := store.Get(ctx, "stats-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.FireCount != 42 {
		t.Errorf("FireCount: got %d, want 42", retrieved.FireCount)
	}
	if retrieved.ErrorCount != 3 {
		t.Errorf("ErrorCount: got %d, want 3", retrieved.ErrorCount)
	}
	if retrieved.LastError != "test error" {
		t.Errorf("LastError: got %s, want 'test error'", retrieved.LastError)
	}
	if retrieved.LastFiredAt == nil {
		t.Error("LastFiredAt was not persisted")
	}
}
