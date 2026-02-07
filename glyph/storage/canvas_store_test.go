package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestCanvasStore_UpsertGlyph(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	glyph := &CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	err := store.UpsertGlyph(ctx, glyph)
	if err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	retrieved, err := store.GetGlyph(ctx, "glyph-1")
	if err != nil {
		t.Fatalf("GetGlyph failed: %v", err)
	}

	if retrieved.ID != glyph.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, glyph.ID)
	}
	if retrieved.Symbol != glyph.Symbol {
		t.Errorf("Symbol mismatch: got %s, want %s", retrieved.Symbol, glyph.Symbol)
	}
	if retrieved.X != glyph.X {
		t.Errorf("X mismatch: got %d, want %d", retrieved.X, glyph.X)
	}
	if retrieved.Y != glyph.Y {
		t.Errorf("Y mismatch: got %d, want %d", retrieved.Y, glyph.Y)
	}
}

func TestCanvasStore_UpsertGlyph_Update(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	glyph := &CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertGlyph(ctx, glyph); err != nil {
		t.Fatalf("UpsertGlyph (create) failed: %v", err)
	}

	glyph.X = 300
	glyph.Y = 400
	glyph.Symbol = "🝓"

	if err := store.UpsertGlyph(ctx, glyph); err != nil {
		t.Fatalf("UpsertGlyph (update) failed: %v", err)
	}

	retrieved, err := store.GetGlyph(ctx, "glyph-1")
	if err != nil {
		t.Fatalf("GetGlyph failed: %v", err)
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

	glyphs, err := store.ListGlyphs(ctx)
	if err != nil {
		t.Fatalf("ListGlyphs failed: %v", err)
	}
	if len(glyphs) != 1 {
		t.Errorf("Upsert created duplicate: got %d glyphs, want 1", len(glyphs))
	}
}

func TestCanvasStore_UpsertGlyph_WithDimensions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	width := 120
	height := 80
	glyph := &CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🝗",
		X:      100,
		Y:      200,
		Width:  &width,
		Height: &height,
	}

	if err := store.UpsertGlyph(ctx, glyph); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	retrieved, err := store.GetGlyph(ctx, "glyph-1")
	if err != nil {
		t.Fatalf("GetGlyph failed: %v", err)
	}

	if retrieved.Width == nil || *retrieved.Width != 120 {
		t.Error("Width not persisted correctly")
	}
	if retrieved.Height == nil || *retrieved.Height != 80 {
		t.Error("Height not persisted correctly")
	}
}

func TestCanvasStore_GetGlyph_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	_, err := store.GetGlyph(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent glyph, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_ListGlyphs(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	glyphs := []*CanvasGlyph{
		{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200},
		{ID: "glyph-3", Symbol: "🝗", X: 300, Y: 300},
	}

	for _, g := range glyphs {
		if err := store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	retrieved, err := store.ListGlyphs(ctx)
	if err != nil {
		t.Fatalf("ListGlyphs failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Fatalf("Expected 3 glyphs, got %d", len(retrieved))
	}
}

func TestCanvasStore_ListGlyphs_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	glyphs, err := store.ListGlyphs(ctx)
	if err != nil {
		t.Fatalf("ListGlyphs failed: %v", err)
	}

	if len(glyphs) != 0 {
		t.Errorf("Expected 0 glyphs, got %d", len(glyphs))
	}
}

func TestCanvasStore_DeleteGlyph(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	glyph := &CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertGlyph(ctx, glyph); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	if err := store.DeleteGlyph(ctx, "glyph-1"); err != nil {
		t.Fatalf("DeleteGlyph failed: %v", err)
	}

	_, err := store.GetGlyph(ctx, "glyph-1")
	if err == nil {
		t.Error("Expected error when getting deleted glyph, got nil")
	}

	glyphs, err := store.ListGlyphs(ctx)
	if err != nil {
		t.Fatalf("ListGlyphs failed: %v", err)
	}
	if len(glyphs) != 0 {
		t.Errorf("Expected 0 glyphs after deletion, got %d", len(glyphs))
	}
}

func TestCanvasStore_DeleteGlyph_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	err := store.DeleteGlyph(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when deleting nonexistent glyph, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestCanvasStore_UpsertComposition(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &CanvasComposition{
		ID:          "comp-1",
		Type:        "ax-prompt",
		InitiatorID: "glyph-1",
		TargetID:    "glyph-2",
		X:           150,
		Y:           150,
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
	if retrieved.Type != comp.Type {
		t.Errorf("Type mismatch: got %s, want %s", retrieved.Type, comp.Type)
	}
	if retrieved.InitiatorID != comp.InitiatorID {
		t.Errorf("InitiatorID mismatch: got %s, want %s", retrieved.InitiatorID, comp.InitiatorID)
	}
	if retrieved.TargetID != comp.TargetID {
		t.Errorf("TargetID mismatch: got %s, want %s", retrieved.TargetID, comp.TargetID)
	}
}

func TestCanvasStore_UpsertComposition_Update(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &CanvasComposition{
		ID:          "comp-1",
		Type:        "ax-prompt",
		InitiatorID: "glyph-1",
		TargetID:    "glyph-2",
		X:           150,
		Y:           150,
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition (create) failed: %v", err)
	}

	comp.X = 250
	comp.Y = 350
	comp.Type = "ax-py"

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
	if retrieved.Type != "ax-py" {
		t.Errorf("Type not updated: got %s, want ax-py", retrieved.Type)
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

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "g1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "g2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "g3", Symbol: "🝗", X: 300, Y: 300}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comps := []*CanvasComposition{
		{ID: "comp-1", Type: "ax-prompt", InitiatorID: "g1", TargetID: "g2", X: 100, Y: 100},
		{ID: "comp-2", Type: "ax-py", InitiatorID: "g2", TargetID: "g3", X: 200, Y: 200},
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

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, &CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &CanvasComposition{
		ID:          "comp-1",
		Type:        "py-prompt",
		InitiatorID: "glyph-1",
		TargetID:    "glyph-2",
		X:           100,
		Y:           200,
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
	glyph := &CanvasGlyph{
		ID:        "glyph-1",
		Symbol:    "🜶",
		X:         100,
		Y:         200,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.UpsertGlyph(ctx, glyph); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	retrieved, err := store.GetGlyph(ctx, "glyph-1")
	if err != nil {
		t.Fatalf("GetGlyph failed: %v", err)
	}

	if !retrieved.CreatedAt.Equal(glyph.CreatedAt) {
		t.Errorf("CreatedAt timestamp lost precision: got %v, want %v", retrieved.CreatedAt, glyph.CreatedAt)
	}
	if !retrieved.UpdatedAt.Equal(glyph.UpdatedAt) {
		t.Errorf("UpdatedAt timestamp lost precision: got %v, want %v", retrieved.UpdatedAt, glyph.UpdatedAt)
	}
}

func TestCanvasStore_ForeignKeyConstraints(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewCanvasStore(db)
	ctx := context.Background()

	// Test 1: Cannot create composition with non-existent glyph IDs
	orphanedComp := &CanvasComposition{
		ID:          "comp-orphaned",
		Type:        "ax-prompt",
		InitiatorID: "nonexistent-glyph-1",
		TargetID:    "nonexistent-glyph-2",
		X:           100,
		Y:           100,
	}

	err := store.UpsertComposition(ctx, orphanedComp)
	if err == nil {
		t.Error("Expected foreign key constraint violation when creating composition with non-existent glyphs, got nil")
	}

	// Test 2: Cascade delete - deleting a glyph should delete its compositions
	glyph1 := &CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      100,
	}
	glyph2 := &CanvasGlyph{
		ID:     "glyph-2",
		Symbol: "🝓",
		X:      200,
		Y:      200,
	}

	if err := store.UpsertGlyph(ctx, glyph1); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(ctx, glyph2); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &CanvasComposition{
		ID:          "comp-1",
		Type:        "ax-prompt",
		InitiatorID: "glyph-1",
		TargetID:    "glyph-2",
		X:           150,
		Y:           150,
	}

	if err := store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	// Verify composition exists
	_, err = store.GetComposition(ctx, "comp-1")
	if err != nil {
		t.Fatalf("GetComposition failed: %v", err)
	}

	// Delete initiator glyph - composition should cascade delete
	if err := store.DeleteGlyph(ctx, "glyph-1"); err != nil {
		t.Fatalf("DeleteGlyph failed: %v", err)
	}

	// Verify composition was cascade deleted
	_, err = store.GetComposition(ctx, "comp-1")
	if err == nil {
		t.Error("Expected composition to be cascade deleted when initiator glyph was deleted, but it still exists")
	}

	// Verify only glyph-2 remains
	glyphs, err := store.ListGlyphs(ctx)
	if err != nil {
		t.Fatalf("ListGlyphs failed: %v", err)
	}
	if len(glyphs) != 1 {
		t.Errorf("Expected 1 remaining glyph, got %d", len(glyphs))
	}
	if len(glyphs) == 1 && glyphs[0].ID != "glyph-2" {
		t.Errorf("Expected remaining glyph to be glyph-2, got %s", glyphs[0].ID)
	}
}
