package sync

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

func TestContentHash_Deterministic(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	as := &types.As{
		ID:         "as-abc123",
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team-eng"},
		Actors:     []string{"hr-system"},
		Timestamp:  ts,
		Source:     "cli",
	}

	h1 := ContentHash(as)
	h2 := ContentHash(as)

	if h1 != h2 {
		t.Fatalf("same attestation produced different hashes: %x vs %x", h1, h2)
	}
}

func TestContentHash_OrderIndependent(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	a := &types.As{
		Subjects:   []string{"b", "a"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
	}

	b := &types.As{
		Subjects:   []string{"a", "b"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
	}

	if ContentHash(a) != ContentHash(b) {
		t.Fatal("identical attestations with different field order produced different hashes")
	}
}

func TestContentHash_DifferentContent(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	base := types.As{
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
	}

	tests := []struct {
		name   string
		mutate func(*types.As)
	}{
		{"different subject", func(a *types.As) { a.Subjects = []string{"user-2"} }},
		{"different predicate", func(a *types.As) { a.Predicates = []string{"admin"} }},
		{"different context", func(a *types.As) { a.Contexts = []string{"team-ops"} }},
		{"different actor", func(a *types.As) { a.Actors = []string{"other-sys"} }},
		{"different timestamp", func(a *types.As) { a.Timestamp = ts.Add(time.Second) }},
		{"different source", func(a *types.As) { a.Source = "api" }},
	}

	baseHash := ContentHash(&base)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modified := base
			tt.mutate(&modified)
			if ContentHash(&modified) == baseHash {
				t.Fatalf("different attestation produced same hash")
			}
		})
	}
}

func TestContentHash_IgnoresASID(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	a := &types.As{
		ID:         "as-111",
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
	}

	b := &types.As{
		ID:         "as-999",
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
	}

	if ContentHash(a) != ContentHash(b) {
		t.Fatal("different ASIDs should not affect content hash")
	}
}

func TestContentHash_IgnoresAttributes(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	a := &types.As{
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
		Attributes: map[string]interface{}{"color": "red"},
	}

	b := &types.As{
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team"},
		Actors:     []string{"sys"},
		Timestamp:  ts,
		Source:     "cli",
		Attributes: nil,
	}

	if ContentHash(a) != ContentHash(b) {
		t.Fatal("attributes should not affect content hash")
	}
}

func TestCanonical_DoesNotMutateInput(t *testing.T) {
	input := []string{"c", "a", "b"}
	canonical(input)
	if input[0] != "c" || input[1] != "a" || input[2] != "b" {
		t.Fatalf("canonical mutated input: got %v", input)
	}
}
