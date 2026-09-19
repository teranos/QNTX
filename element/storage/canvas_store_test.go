package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/teranos/QNTX/element/proto"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// Helper function to create edges for testing
func makeEdge(from, to, direction string, position int32) *pb.CompositionEdge {
	return &pb.CompositionEdge{
		From:      from,
		To:        to,
		Direction: direction,
		Position:  position,
	}
}

func TestCanvasStore_UpsertElement(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	item := &CanvasElement{
		ID:     "element-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	err := store.UpsertElement(ctx, item)
	if err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	retrieved, err := store.GetElement(ctx, "element-1")
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if retrieved.ID != item.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, item.ID)
	}
	if retrieved.Symbol != item.Symbol {
		t.Errorf("Symbol mismatch: got %s, want %s", retrieved.Symbol, item.Symbol)
	}
	if retrieved.X != item.X {
		t.Errorf("X mismatch: got %d, want %d", retrieved.X, item.X)
	}
	if retrieved.Y != item.Y {
		t.Errorf("Y mismatch: got %d, want %d", retrieved.Y, item.Y)
	}
}

func TestCanvasStore_UpsertElement_Update(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	item := &CanvasElement{
		ID:     "element-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertElement(ctx, item); err != nil {
		t.Fatalf("UpsertElement (create) failed: %v", err)
	}

	item.X = 300
	item.Y = 400
	item.Symbol = "🝓"

	if err := store.UpsertElement(ctx, item); err != nil {
		t.Fatalf("UpsertElement (update) failed: %v", err)
	}

	retrieved, err := store.GetElement(ctx, "element-1")
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if retrieved.X != 300 {
		t.Errorf("X not updated: got %d, want 300", retrieved.X)
	}
	if retrieved.Y != 400 {
		t.Errorf("Y not updated: got %d, want 400", retrieved.Y)
	}
	if retrieved.Symbol != "🝓" {
		t.Errorf("Symbol not updated: got %s, want 🝓", retrieved.Symbol)
	}

	items, err := store.ListElements(ctx)
	if err != nil {
		t.Fatalf("ListElements failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("Upsert created duplicate: got %d elements, want 1", len(items))
	}
}

func TestCanvasStore_UpsertElement_WithDimensions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	width := int32(120)
	height := int32(80)
	item := &CanvasElement{
		ID:     "element-1",
		Symbol: "🝗",
		X:      100,
		Y:      200,
		Width:  &width,
		Height: &height,
	}

	if err := store.UpsertElement(ctx, item); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	retrieved, err := store.GetElement(ctx, "element-1")
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if retrieved.Width == nil || *retrieved.Width != 120 {
		t.Error("Width not persisted correctly")
	}
	if retrieved.Height == nil || *retrieved.Height != 80 {
		t.Error("Height not persisted correctly")
	}
}

func TestCanvasStore_GetElement_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	_, err := store.GetElement(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent element, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_ListElements(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	items := []*CanvasElement{
		{ID: "element-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "element-2", Symbol: "🝓", X: 200, Y: 200},
		{ID: "element-3", Symbol: "🝗", X: 300, Y: 300},
	}

	for _, g := range items {
		if err := store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	retrieved, err := store.ListElements(ctx)
	if err != nil {
		t.Fatalf("ListElements failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Fatalf("Expected 3 elements, got %d", len(retrieved))
	}
}

func TestCanvasStore_ListElements_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	items, err := store.ListElements(ctx)
	if err != nil {
		t.Fatalf("ListElements failed: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("Expected 0 elements, got %d", len(items))
	}
}

func TestCanvasStore_DeleteElement(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	item := &CanvasElement{
		ID:     "element-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertElement(ctx, item); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	if err := store.DeleteElement(ctx, "element-1"); err != nil {
		t.Fatalf("DeleteElement failed: %v", err)
	}

	_, err := store.GetElement(ctx, "element-1")
	if err == nil {
		t.Error("Expected error when getting deleted element, got nil")
	}

	items, err := store.ListElements(ctx)
	if err != nil {
		t.Fatalf("ListElements failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 elements after deletion, got %d", len(items))
	}
}

func TestCanvasStore_DeleteElement_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	err := store.DeleteElement(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when deleting nonexistent element, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_UpsertComposition(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 150,
		Y: 150,
	}

	err := store.UpsertComposition(ctx, comp)
	if err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	retrieved, err := store.GetComposition(ctx, "comp-1")
	if err != nil {
		t.Fatalf("GetComposition failed: %v", err)
	}

	if retrieved.ID != comp.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, comp.ID)
	}
	if len(retrieved.Edges) != len(comp.Edges) {
		t.Errorf("Edges length mismatch: got %d, want %d", len(retrieved.Edges), len(comp.Edges))
	}
	for i, edge := range comp.Edges {
		if retrieved.Edges[i].From != edge.From {
			t.Errorf("Edge[%d].From mismatch: got %s, want %s", i, retrieved.Edges[i].From, edge.From)
		}
		if retrieved.Edges[i].To != edge.To {
			t.Errorf("Edge[%d].To mismatch: got %s, want %s", i, retrieved.Edges[i].To, edge.To)
		}
		if retrieved.Edges[i].Direction != edge.Direction {
			t.Errorf("Edge[%d].Direction mismatch: got %s, want %s", i, retrieved.Edges[i].Direction, edge.Direction)
		}
	}
}

func TestCanvasStore_UpsertComposition_Update(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 150,
		Y: 150,
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition (create) failed: %v", err)
	}

	// Update position and edges
	comp.X = 250
	comp.Y = 350
	comp.Edges = []*pb.CompositionEdge{
		makeEdge("element-2", "element-1", "right", 0), // reversed direction
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition (update) failed: %v", err)
	}

	retrieved, err := store.GetComposition(ctx, "comp-1")
	if err != nil {
		t.Fatalf("GetComposition failed: %v", err)
	}

	if retrieved.X != 250 {
		t.Errorf("X not updated: got %d, want 250", retrieved.X)
	}
	if retrieved.Y != 350 {
		t.Errorf("Y not updated: got %d, want 350", retrieved.Y)
	}
	if len(retrieved.Edges) != 1 {
		t.Errorf("Edges not updated: got %d edges, want 1", len(retrieved.Edges))
	}
	if retrieved.Edges[0].From != "element-2" {
		t.Errorf("Edge.From not updated: got %s, want element-2", retrieved.Edges[0].From)
	}

	comps, err := store.ListCompositions(ctx)
	if err != nil {
		t.Fatalf("ListCompositions failed: %v", err)
	}
	if len(comps) != 1 {
		t.Errorf("Upsert created duplicate: got %d compositions, want 1", len(comps))
	}
}

func TestCanvasStore_GetComposition_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	_, err := store.GetComposition(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent composition, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_ListCompositions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "g1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "g2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "g3", Symbol: "🝗", X: 300, Y: 300}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comps := []*CanvasComposition{
		{
			ID:    "comp-1",
			Edges: []*pb.CompositionEdge{makeEdge("g1", "g2", "right", 0)},
			X:     100,
			Y:     100,
		},
		{
			ID:    "comp-2",
			Edges: []*pb.CompositionEdge{makeEdge("g2", "g3", "right", 0)},
			X:     200,
			Y:     200,
		},
	}

	for _, c := range comps {
		if err := store.UpsertComposition(ctx, c); err != nil {
			t.Fatalf("UpsertComposition failed: %v", err)
		}
	}

	retrieved, err := store.ListCompositions(ctx)
	if err != nil {
		t.Fatalf("ListCompositions failed: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("Expected 2 compositions, got %d", len(retrieved))
	}
}

func TestCanvasStore_ListCompositions_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	comps, err := store.ListCompositions(ctx)
	if err != nil {
		t.Fatalf("ListCompositions failed: %v", err)
	}

	if len(comps) != 0 {
		t.Errorf("Expected 0 compositions, got %d", len(comps))
	}
}

func TestCanvasStore_DeleteComposition(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, &CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 100,
		Y: 200,
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	if err := store.DeleteComposition(ctx, "comp-1"); err != nil {
		t.Fatalf("DeleteComposition failed: %v", err)
	}

	_, err := store.GetComposition(ctx, "comp-1")
	if err == nil {
		t.Error("Expected error when getting deleted composition, got nil")
	}

	comps, err := store.ListCompositions(ctx)
	if err != nil {
		t.Fatalf("ListCompositions failed: %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("Expected 0 compositions after deletion, got %d", len(comps))
	}
}

func TestCanvasStore_DeleteComposition_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	err := store.DeleteComposition(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when deleting nonexistent composition, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_Timestamps(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	now := time.Now()
	item := &CanvasElement{
		ID:        "element-1",
		Symbol:    "🜶",
		X:         100,
		Y:         200,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.UpsertElement(ctx, item); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	retrieved, err := store.GetElement(ctx, "element-1")
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if !retrieved.CreatedAt.Equal(item.CreatedAt) {
		t.Errorf("CreatedAt timestamp lost precision: got %v, want %v", retrieved.CreatedAt, item.CreatedAt)
	}
	if !retrieved.UpdatedAt.Equal(item.UpdatedAt) {
		t.Errorf("UpdatedAt timestamp lost precision: got %v, want %v", retrieved.UpdatedAt, item.UpdatedAt)
	}
}

// === Minimized window tests ===

func TestCanvasStore_AddMinimizedWindow(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	err := store.AddMinimizedWindow(ctx, "element-1")
	if err != nil {
		t.Fatalf("AddMinimizedWindow failed: %v", err)
	}

	windows, err := store.ListMinimizedWindows(ctx)
	if err != nil {
		t.Fatalf("ListMinimizedWindows failed: %v", err)
	}

	if len(windows) != 1 {
		t.Fatalf("Expected 1 minimized window, got %d", len(windows))
	}
	if windows[0].ElementID != "element-1" {
		t.Errorf("ElementID mismatch: got %s, want element-1", windows[0].ElementID)
	}
}

func TestCanvasStore_AddMinimizedWindow_Idempotent(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Add same ID twice — INSERT OR IGNORE should succeed silently
	if err := store.AddMinimizedWindow(ctx, "element-1"); err != nil {
		t.Fatalf("First AddMinimizedWindow failed: %v", err)
	}
	if err := store.AddMinimizedWindow(ctx, "element-1"); err != nil {
		t.Fatalf("Second AddMinimizedWindow failed: %v", err)
	}

	windows, err := store.ListMinimizedWindows(ctx)
	if err != nil {
		t.Fatalf("ListMinimizedWindows failed: %v", err)
	}

	if len(windows) != 1 {
		t.Errorf("Expected 1 minimized window (idempotent), got %d", len(windows))
	}
}

func TestCanvasStore_ListMinimizedWindows_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	windows, err := store.ListMinimizedWindows(ctx)
	if err != nil {
		t.Fatalf("ListMinimizedWindows failed: %v", err)
	}

	if len(windows) != 0 {
		t.Errorf("Expected 0 minimized windows, got %d", len(windows))
	}
}

func TestCanvasStore_RemoveMinimizedWindow(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	if err := store.AddMinimizedWindow(ctx, "element-1"); err != nil {
		t.Fatalf("AddMinimizedWindow failed: %v", err)
	}

	if err := store.RemoveMinimizedWindow(ctx, "element-1"); err != nil {
		t.Fatalf("RemoveMinimizedWindow failed: %v", err)
	}

	windows, err := store.ListMinimizedWindows(ctx)
	if err != nil {
		t.Fatalf("ListMinimizedWindows failed: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("Expected 0 minimized windows after removal, got %d", len(windows))
	}
}

func TestCanvasStore_RemoveMinimizedWindow_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	err := store.RemoveMinimizedWindow(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when removing nonexistent minimized window, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

// TestCanvasStore_OrphanedCompositions tests that compositions with non-existent elements
// are handled gracefully. Migration 047 relaxed FK constraints on element IDs to support
// eventual consistency in the sync queue — compositions may sync before their member elements.
func TestCanvasStore_OrphanedCompositions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Compositions referencing non-existent elements are allowed (no FK on element IDs)
	orphanedComp := &CanvasComposition{
		ID: "comp-orphaned",
		Edges: []*pb.CompositionEdge{
			makeEdge("nonexistent-element-1", "nonexistent-element-2", "right", 0),
		},
		X: 100,
		Y: 100,
	}

	err := store.UpsertComposition(ctx, orphanedComp)
	if err != nil {
		t.Fatalf("UpsertComposition should succeed with non-existent elements (relaxed FKs): %v", err)
	}

	// Composition exists and retains its edges even without the elements
	got, err := store.GetComposition(ctx, "comp-orphaned")
	if err != nil {
		t.Fatalf("GetComposition failed: %v", err)
	}
	if len(got.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(got.Edges))
	}

	// Deleting elements does not cascade to compositions (no FK)
	element1 := &CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}
	element2 := &CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}

	if err := store.UpsertElement(ctx, element1); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(ctx, element2); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 150,
		Y: 150,
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	// Delete both elements — composition and edges remain (no cascade)
	if err := store.DeleteElement(ctx, "element-1"); err != nil {
		t.Fatalf("DeleteElement failed: %v", err)
	}
	if err := store.DeleteElement(ctx, "element-2"); err != nil {
		t.Fatalf("DeleteElement failed: %v", err)
	}

	// Composition still exists with its edges (orphaned but intact)
	got, err = store.GetComposition(ctx, "comp-1")
	if err != nil {
		t.Fatalf("Composition should still exist after element deletion (relaxed FKs): %v", err)
	}
	if len(got.Edges) != 1 {
		t.Errorf("Expected 1 edge still present, got %d", len(got.Edges))
	}

	// Verify elements are actually deleted
	items, err := store.ListElements(ctx)
	if err != nil {
		t.Fatalf("ListElements failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 remaining elements, got %d", len(items))
	}
}
