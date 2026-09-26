package storage_test

import (
	"strings"
	"testing"

	elementstorage "github.com/teranos/QNTX/element/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/errors"
)

// "given a namespace there can be multiple canvasses"
func TestANamespaceHoldsManyCanvasesAndEachKeepsItsOwnElements(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	ctx := t.Context()

	if err := store.Create(ctx, "garden", "US-ROOT", "root"); err != nil {
		t.Fatal(err)
	}
	alices, err := store.CreateCanvas(ctx, "alice's", elementstorage.CanvasOfAUser, "US-ALICE", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if alices.Owners[0] != "US-ALICE" {
		t.Fatalf("alice does not own her canvas: %v", alices.Owners)
	}

	// "a new canvas, should always start with a prefilled Note element"
	opened, err := store.In(alices.ID).ListElements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0].Symbol != elementstorage.ProseSymbol || opened[0].Content == nil {
		t.Fatalf("a new canvas did not open with its note: %+v", opened)
	}
	if !strings.Contains(*opened[0].Content, "alice's canvas element.") || !strings.Contains(*opened[0].Content, "Created by: Alice") {
		t.Fatalf("the note does not say what it should: %q", *opened[0].Content)
	}

	if err := store.In(alices.ID).UpsertElement(ctx, &elementstorage.CanvasElement{ID: "only-alices", Symbol: "⋈"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetElement(ctx, "only-alices"); !errors.Is(err, elementstorage.ErrNotFound) {
		t.Fatalf("the namespace's canvas sees alice's element: %v", err)
	}
	if _, err := store.In(alices.ID).GetElement(ctx, "only-alices"); err != nil {
		t.Fatalf("alice's canvas lost her element: %v", err)
	}

	all, err := store.Canvases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 canvases, got %d", len(all))
	}
}

// "when anyone deletes a canvas, its not actually deleted, its disabled."
func TestDeleteIsDisableAndTheCanvasStays(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	ctx := t.Context()
	c, err := store.CreateCanvas(ctx, "bob's", elementstorage.CanvasOfAUser, "US-BOB", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Disable(ctx, c.ID, "US-SUPER"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Canvas(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledBy != "US-SUPER" {
		t.Fatalf("disabled_by = %q", got.DisabledBy)
	}
	if err := store.Enable(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Canvas(ctx, c.ID)
	if got.Disabled() {
		t.Fatal("still disabled after enable")
	}
}

// "a canvas can be nuked when disabled, like we do with namespaces themselves."
func TestOnlyADisabledCanvasIsNukedAndItsElementsGoWithIt(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	ctx := t.Context()
	c, err := store.CreateCanvas(ctx, "bob's", elementstorage.CanvasOfAUser, "US-BOB", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Nuke(ctx, c.ID); !errors.Is(err, elementstorage.ErrNotDisabled) {
		t.Fatalf("an enabled canvas was nuked: %v", err)
	}
	if err := store.Disable(ctx, c.ID, "US-ROOT"); err != nil {
		t.Fatal(err)
	}
	if err := store.Nuke(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Canvas(ctx, c.ID); !errors.Is(err, elementstorage.ErrNoSuchCanvas) {
		t.Fatalf("the canvas is still there: %v", err)
	}
	left, err := store.In(c.ID).ListElements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("elements outlived their canvas: %d", len(left))
	}
}

// "A canvas can have multiple owners" — by invitation, accepted by the one invited.
func TestAnInvitationAcceptedMakesAnOwner(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	ctx := t.Context()
	c, err := store.CreateCanvas(ctx, "alice's", elementstorage.CanvasOfAUser, "US-ALICE", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.Invite(ctx, c.ID, "US-ALICE", "US-BOB", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ctx, inv.Token, "US-CAROL"); err == nil {
		t.Fatal("carol accepted bob's invitation")
	}
	if _, err := store.Accept(ctx, inv.Token, "US-BOB"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ctx, inv.Token, "US-BOB"); !errors.Is(err, elementstorage.ErrNoSuchInvitation) {
		t.Fatalf("an invitation was accepted twice: %v", err)
	}
	got, _ := store.Canvas(ctx, c.ID)
	if len(got.Owners) != 2 {
		t.Fatalf("owners = %v", got.Owners)
	}
	if err := store.Disown(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Canvas(ctx, c.ID)
	if len(got.Owners) != 0 {
		t.Fatalf("still owned after disown: %v", got.Owners)
	}
}
