package watcher

import (
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

func aStoodRow(id string) storage.Watcher {
	return storage.Watcher{
		ID:                id,
		Name:              id,
		Filter:            types.AxFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{"stood:" + id}},
		ActionType:        storage.ActionTypeBuiltinExecute,
		ActionData:        `{"handler_name":"stood.row"}`,
		MaxFiresPerSecond: 10,
		Enabled:           true,
	}
}

// A subsystem compiled in stands its rows beside QNTX's own: every namespace
// watches them from then on, and a stored row cannot take them away either.
func TestARowStoodIsStanding(t *testing.T) {
	t.Cleanup(func() { stood = nil })
	before := len(Standing())
	if err := Stand(aStoodRow("stood-one")); err != nil {
		t.Fatal(err)
	}
	if !IsStanding("stood-one") {
		t.Fatal("a row stood is not standing")
	}
	if got := len(Standing()); got != before+1 {
		t.Fatalf("Standing holds %d rows, want %d", got, before+1)
	}
}

// A different row under the id of one already standing, QNTX's own or a
// subsystem's, is refused by that id: two rows of one id would be one the
// engine reports and one it does not. The same row stood again is the same
// build booting again in one process, and stands once.
func TestADifferentRowUnderAStandingIDIsRefused(t *testing.T) {
	t.Cleanup(func() { stood = nil })
	if err := Stand(aStoodRow(StandingCIPushed)); err == nil || !strings.Contains(err.Error(), StandingCIPushed) {
		t.Fatalf("a row with QNTX's own id: %v", err)
	}
	if err := Stand(aStoodRow("stood-twice")); err != nil {
		t.Fatal(err)
	}
	if err := Stand(aStoodRow("stood-twice")); err != nil {
		t.Fatalf("the same row stood again: %v", err)
	}
	if n := len(stood); n != 1 {
		t.Fatalf("the same row stands %d times", n)
	}
	other := aStoodRow("stood-twice")
	other.ActionData = `{"handler_name":"somebody.else"}`
	if err := Stand(other); err == nil || !strings.Contains(err.Error(), "stood-twice") {
		t.Fatalf("a different row under a standing id: %v", err)
	}
}

// What Standing hands out of a stood row is a copy too.
func TestAStoodRowCannotBeEditedThroughWhatStandingHandsOut(t *testing.T) {
	t.Cleanup(func() { stood = nil })
	if err := Stand(aStoodRow("stood-copy")); err != nil {
		t.Fatal(err)
	}
	held := Standing()
	last := held[len(held)-1]
	last.Name = "something else"
	again := Standing()
	if again[len(again)-1].Name != "stood-copy" {
		t.Errorf("the stood row was edited through what Standing handed out: %q", again[len(again)-1].Name)
	}
}
