package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage/testutil"
	"github.com/teranos/QNTX/ats/types"
)

// TestNewSymbolIndex_Success tests successful index creation
func TestNewSymbolIndex_Success(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error = %v, want nil", err)
	}

	if idx == nil {
		t.Fatal("NewSymbolIndex() returned nil index")
	}

	// Verify index was refreshed (lastUpdate should be recent)
	if idx.LastUpdate().IsZero() {
		t.Error("Index lastUpdate is zero, expected recent timestamp")
	}
}

// TestNewSymbolIndex_MissingTable tests error handling for missing attestations table
func TestNewSymbolIndex_MissingTable(t *testing.T) {
	db := testutil.SetupEmptyDB(t)
	defer db.Close()

	_, err := NewSymbolIndex(db)
	if err == nil {
		t.Error("NewSymbolIndex() error = nil, want error for missing table")
	}

	if !strings.Contains(err.Error(), "attestations table not found") {
		t.Errorf("Error message = %q, want mention of 'attestations table not found'", err.Error())
	}
}

// TestRefresh tests index refresh functionality
func TestRefresh(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	// Add test attestations
	store := NewSQLStore(db, nil)
	attestations := []types.As{
		{
			ID:         "test-1",
			Subjects:   []string{"alice", "alice-alt"},
			Predicates: []string{"knows"},
			Contexts:   []string{"bob"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		},
		{
			ID:         "test-2",
			Subjects:   []string{"alice"},
			Predicates: []string{"works-at"},
			Contexts:   []string{"acme-corp"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		},
	}

	for i := range attestations {
		if err := store.CreateAttestation(context.Background(), &attestations[i]); err != nil {
			t.Fatalf("Failed to create test attestation: %v", err)
		}
	}

	// Refresh index
	oldUpdate := idx.LastUpdate()
	time.Sleep(10 * time.Millisecond) // Ensure timestamp differs

	err = idx.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	// Verify lastUpdate changed
	if !idx.LastUpdate().After(oldUpdate) {
		t.Error("LastUpdate not updated after Refresh()")
	}

	// Verify symbols were indexed
	count := idx.GetAttestationCount("alice", "subject")
	if count != 2 {
		t.Errorf("GetAttestationCount('alice', 'subject') = %d, want 2", count)
	}
}

// TestGetAttestationCount tests symbol count retrieval
func TestGetAttestationCount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	// Create test data
	store := NewSQLStore(db, nil)
	for i := 0; i < 5; i++ {
		attestation := &types.As{
			ID:         string(rune('a' + i)),
			Subjects:   []string{"alice"},
			Predicates: []string{"knows"},
			Contexts:   []string{"bob"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	tests := []struct {
		symbol     string
		symbolType string
		wantCount  int
	}{
		{"alice", "subject", 5},
		{"knows", "predicate", 5},
		{"bob", "context", 5},
		{"test-actor", "actor", 5},
		{"non-existent", "subject", 0},
		{"alice", "invalid-type", 0},
	}

	for _, tt := range tests {
		t.Run(tt.symbol+"_"+tt.symbolType, func(t *testing.T) {
			count := idx.GetAttestationCount(tt.symbol, tt.symbolType)
			if count != tt.wantCount {
				t.Errorf("GetAttestationCount(%q, %q) = %d, want %d",
					tt.symbol, tt.symbolType, count, tt.wantCount)
			}
		})
	}
}

// TestGetSubjectCompletions tests subject completion with 3-char minimum
func TestGetSubjectCompletions(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	// Create test data with various subjects
	store := NewSQLStore(db, nil)
	subjects := []string{"alice", "alicia", "alex", "bob", "barbara"}
	for _, subject := range subjects {
		attestation := &types.As{
			ID:         subject,
			Subjects:   []string{subject},
			Predicates: []string{"test"},
			Contexts:   []string{"value"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	tests := []struct {
		prefix    string
		wantCount int
		wantEmpty bool
	}{
		{"al", 3, false},   // Matches "alice", "alicia", "alex" (2-char minimum)
		{"ali", 2, false},  // Matches "alice", "alicia"
		{"alex", 1, false}, // Matches "alex"
		{"bob", 1, false},  // Matches "bob"
		{"bar", 1, false},  // Matches "barbara"
		{"xyz", 0, false},  // No matches
		{"", 0, true},      // Empty prefix
		{"a", 0, true},     // Less than 2 chars
	}

	for _, tt := range tests {
		t.Run("prefix_"+tt.prefix, func(t *testing.T) {
			items := idx.GetSubjectCompletions(tt.prefix)

			if tt.wantEmpty && len(items) != 0 {
				t.Errorf("GetSubjectCompletions(%q) returned %d items, want 0 (below min length)",
					tt.prefix, len(items))
			}

			if !tt.wantEmpty && len(items) != tt.wantCount {
				t.Errorf("GetSubjectCompletions(%q) returned %d items, want %d",
					tt.prefix, len(items), tt.wantCount)
			}

			// Verify all items match prefix (case-insensitive)
			for _, item := range items {
				if !strings.HasPrefix(strings.ToLower(item.Label), strings.ToLower(tt.prefix)) {
					t.Errorf("Completion %q does not match prefix %q", item.Label, tt.prefix)
				}
			}
		})
	}
}

// TestGetPredicateCompletions tests predicate completion with 1-char minimum
func TestGetPredicateCompletions(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	// Create test data
	store := NewSQLStore(db, nil)
	predicates := []string{"knows", "works-at", "lives-in"}
	for _, predicate := range predicates {
		attestation := &types.As{
			ID:         predicate,
			Subjects:   []string{"test"},
			Predicates: []string{predicate},
			Contexts:   []string{"value"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	tests := []struct {
		prefix    string
		wantCount int
	}{
		{"k", 1},   // Matches "knows"
		{"w", 1},   // Matches "works-at"
		{"l", 1},   // Matches "lives-in"
		{"kn", 1},  // Matches "knows"
		{"xyz", 0}, // No matches
		{"", 0},    // Empty prefix (below minimum)
	}

	for _, tt := range tests {
		t.Run("prefix_"+tt.prefix, func(t *testing.T) {
			items := idx.GetPredicateCompletions(tt.prefix)

			if len(items) != tt.wantCount {
				t.Errorf("GetPredicateCompletions(%q) returned %d items, want %d",
					tt.prefix, len(items), tt.wantCount)
			}
		})
	}
}

// TestGetContextCompletions tests context completion
func TestGetContextCompletions(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)
	contexts := []string{"seattle", "san-francisco", "new-york"}
	for _, ctx := range contexts {
		attestation := &types.As{
			ID:         ctx,
			Subjects:   []string{"test"},
			Predicates: []string{"lives-in"},
			Contexts:   []string{ctx},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	items := idx.GetContextCompletions("se")
	if len(items) != 1 {
		t.Errorf("GetContextCompletions('se') returned %d items, want 1 (seattle)", len(items))
	}

	if len(items) > 0 && items[0].Label != "seattle" {
		t.Errorf("First completion = %q, want 'seattle'", items[0].Label)
	}
}

// TestGetActorCompletions tests actor completion
func TestGetActorCompletions(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)
	actors := []string{"alice-agent", "bob-bot", "carol-script"}
	for _, actor := range actors {
		attestation := &types.As{
			ID:         actor,
			Subjects:   []string{"test"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Actors:     []string{actor},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	items := idx.GetActorCompletions("al")
	if len(items) != 1 {
		t.Errorf("GetActorCompletions('al') returned %d items, want 1 (alice-agent)", len(items))
	}
}

// TestCompletions_Metadata tests completion item metadata
func TestCompletions_Metadata(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create multiple attestations for same subject to test count
	for i := 0; i < 3; i++ {
		attestation := &types.As{
			ID:         string(rune('a' + i)),
			Subjects:   []string{"alice"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	items := idx.GetSubjectCompletions("ali")
	if len(items) != 1 {
		t.Fatalf("Expected 1 completion, got %d", len(items))
	}

	item := items[0]

	// Verify completion item structure
	if item.Label != "alice" {
		t.Errorf("Label = %q, want 'alice'", item.Label)
	}

	if item.Kind != "subject" {
		t.Errorf("Kind = %q, want 'subject'", item.Kind)
	}

	if item.InsertText != "alice" {
		t.Errorf("InsertText = %q, want 'alice'", item.InsertText)
	}

	if item.Detail != "3 attestations" {
		t.Errorf("Detail = %q, want '3 attestations'", item.Detail)
	}

	// SortText should be lower for higher counts (inverse ranking)
	if item.SortText == "" {
		t.Error("SortText is empty, want numeric value")
	}
}

// TestCompletions_Limit tests 10-item limit
func TestCompletions_Limit(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create 15 subjects starting with "test"
	for i := 0; i < 15; i++ {
		subject := "test" + string(rune('a'+i))
		attestation := &types.As{
			ID:         subject,
			Subjects:   []string{subject},
			Predicates: []string{"pred"},
			Contexts:   []string{"ctx"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(context.Background(), attestation); err != nil {
			t.Fatalf("Failed to create attestation: %v", err)
		}
	}

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	items := idx.GetSubjectCompletions("test")

	// Should be limited to 10 items
	if len(items) > 10 {
		t.Errorf("GetSubjectCompletions returned %d items, want ≤10 (limit)", len(items))
	}
}

// TestLastUpdate tests LastUpdate tracking
func TestLastUpdate(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	idx, err := NewSymbolIndex(db)
	if err != nil {
		t.Fatalf("NewSymbolIndex() error: %v", err)
	}

	// LastUpdate should be recent after NewSymbolIndex
	if time.Since(idx.LastUpdate()) > 5*time.Second {
		t.Error("LastUpdate is too old, expected recent timestamp")
	}

	// Wait and refresh
	time.Sleep(10 * time.Millisecond)
	oldUpdate := idx.LastUpdate()

	if err := idx.Refresh(); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	// LastUpdate should be more recent after Refresh
	if !idx.LastUpdate().After(oldUpdate) {
		t.Error("LastUpdate not updated after Refresh()")
	}
}
