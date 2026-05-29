package storage

import (
	"context"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestEnsureFilesystemCanvas_EmptyDB(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()

	id, err := EnsureFilesystemCanvas(ctx, db, "/some/test/dir")
	if err != nil {
		t.Fatalf("EnsureFilesystemCanvas failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty canvas id")
	}

	var name, anchor string
	if err := db.QueryRowContext(ctx, "SELECT name, anchor FROM canvases WHERE id = ?", id).Scan(&name, &anchor); err != nil {
		t.Fatalf("failed to read canvas row: %v", err)
	}
	if name != "dir" {
		t.Errorf("expected name='dir' (basename of /some/test/dir), got %q", name)
	}
	if anchor != "filesystem" {
		t.Errorf("expected anchor='filesystem', got %q", anchor)
	}
}

func TestEnsureFilesystemCanvas_Idempotent(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()

	var firstID string
	for i := 0; i < 3; i++ {
		id, err := EnsureFilesystemCanvas(ctx, db, "/some/test/dir")
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if i == 0 {
			firstID = id
		} else if id != firstID {
			t.Fatalf("call %d returned different id %q (expected %q)", i, id, firstID)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM canvases").Scan(&count); err != nil {
		t.Fatalf("failed to count canvases: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 canvas after 3 calls, got %d", count)
	}
}
