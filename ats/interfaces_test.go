package ats_test

import (
	"testing"

	"github.com/teranos/QNTX/ats"
)

// TestNoOpEntityResolver_ReturnsEmpty verifies the default entity resolver
// returns no alternative IDs
func TestNoOpEntityResolver_ReturnsEmpty(t *testing.T) {
	resolver := &ats.NoOpEntityResolver{}

	ids, err := resolver.GetAlternativeIDs("test-entity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ids) != 0 {
		t.Errorf("NoOp should return empty alternatives, got %v", ids)
	}
}
