package storage

import (
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)

	// Apply migrations (includes aliases table from migration 009)
	if err := db.Migrate(testDB, nil); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return testDB
}

func TestResolver_CreateAlias(t *testing.T) {
	testDB := setupTestDB(t)
	defer testDB.Close()

	resolver := alias.NewResolver(NewAliasStore(testDB))

	// Test creating a basic alias
	err := resolver.CreateAlias("BILL", "WILLIAM SMITH")
	if err != nil {
		t.Errorf("Expected no error creating alias, got: %v", err)
	}

	// Test creating another alias for the same person
	err = resolver.CreateAlias("BILL", "W.SMITH")
	if err != nil {
		t.Errorf("Expected no error creating second alias, got: %v", err)
	}
}

func TestResolver_ResolveIdentifier(t *testing.T) {
	testDB := setupTestDB(t)
	defer testDB.Close()

	resolver := alias.NewResolver(NewAliasStore(testDB))

	// Create aliases
	_ = resolver.CreateAlias("BILL", "WILLIAM SMITH")
	_ = resolver.CreateAlias("BILL", "W.SMITH")

	tests := []struct {
		identifier string
		expected   int // number of identifiers that should be returned
	}{
		{"BILL", 3},          // Should return BILL, WILLIAM SMITH, W.SMITH
		{"WILLIAM SMITH", 2}, // Should return WILLIAM SMITH, BILL (not transitive)
		{"W.SMITH", 2},       // Should return W.SMITH, BILL (not transitive)
		{"NONEXISTENT", 1},   // Should return just itself
	}

	for _, test := range tests {
		resolved, err := resolver.ResolveIdentifier(context.Background(), test.identifier)
		if err != nil {
			t.Errorf("Error resolving %s: %v", test.identifier, err)
			continue
		}

		if len(resolved) != test.expected {
			t.Errorf("Expected %d resolved identifiers for %s, got %d: %v",
				test.expected, test.identifier, len(resolved), resolved)
		}

		// Should always include the original identifier
		found := false
		for _, id := range resolved {
			if id == test.identifier {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Resolved identifiers for %s should include original identifier", test.identifier)
		}
	}
}

func TestResolver_GetAliasesFor(t *testing.T) {
	testDB := setupTestDB(t)
	defer testDB.Close()

	resolver := alias.NewResolver(NewAliasStore(testDB))

	// Create aliases
	_ = resolver.CreateAlias("ALICE", "ALICE SMITH")
	_ = resolver.CreateAlias("ALICE", "A.SMITH")

	aliases, err := resolver.GetAliasesFor(context.Background(), "ALICE")
	if err != nil {
		t.Errorf("Error getting aliases: %v", err)
	}

	expectedAliases := []string{"ALICE SMITH", "A.SMITH"}
	if len(aliases) != len(expectedAliases) {
		t.Errorf("Expected %d aliases, got %d: %v", len(expectedAliases), len(aliases), aliases)
	}

	// Should not include the original identifier
	for _, alias := range aliases {
		if alias == "ALICE" {
			t.Errorf("GetAliasesFor should not return the original identifier")
		}
	}

	// Test with non-existent identifier
	aliases, err = resolver.GetAliasesFor(context.Background(), "NONEXISTENT")
	if err != nil {
		t.Errorf("Error getting aliases for nonexistent: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("Expected no aliases for nonexistent identifier, got: %v", aliases)
	}
}

func TestResolver_RemoveAlias(t *testing.T) {
	testDB := setupTestDB(t)
	defer testDB.Close()

	resolver := alias.NewResolver(NewAliasStore(testDB))

	// Create and then remove an alias
	_ = resolver.CreateAlias("BOB", "ROBERT JONES")
	_ = resolver.CreateAlias("BOB", "R.JONES")

	// Verify it was created
	resolved, _ := resolver.ResolveIdentifier(context.Background(), "BOB")
	if len(resolved) != 3 {
		t.Errorf("Expected 3 resolved identifiers before removal, got %d", len(resolved))
	}

	// Remove one alias
	err := resolver.RemoveAlias("BOB", "ROBERT JONES")
	if err != nil {
		t.Errorf("Error removing alias: %v", err)
	}

	// Verify it was removed
	resolved, _ = resolver.ResolveIdentifier(context.Background(), "BOB")
	if len(resolved) != 2 {
		t.Errorf("Expected 2 resolved identifiers after removal, got %d", len(resolved))
	}

	// Should still have BOB and R.JONES
	expectedRemaining := map[string]bool{"BOB": true, "R.JONES": true}
	for _, id := range resolved {
		if !expectedRemaining[id] {
			t.Errorf("Unexpected identifier after removal: %s", id)
		}
	}
}

func TestResolver_GetAllAliases(t *testing.T) {
	testDB := setupTestDB(t)
	defer testDB.Close()

	resolver := alias.NewResolver(NewAliasStore(testDB))

	// Create several aliases
	_ = resolver.CreateAlias("CHARLIE", "CHARLES BROWN")
	_ = resolver.CreateAlias("DAVE", "DAVID GREEN")

	allAliases, err := resolver.GetAllAliases()
	if err != nil {
		t.Errorf("Error getting all aliases: %v", err)
	}

	// Should have entries for both sets of aliases
	if len(allAliases) < 2 {
		t.Errorf("Expected at least 2 alias groups, got %d", len(allAliases))
	}

	// GetAllAliases returns direct mappings (alias -> target), not grouped aliases
	// So we just verify we have some aliases and they're formatted correctly
	totalMappings := 0
	for identifier, targets := range allAliases {
		if len(targets) < 1 {
			t.Errorf("Alias mapping for %s should have at least 1 target", identifier)
		}
		totalMappings += len(targets)
	}

	if totalMappings < 4 {
		t.Errorf("Expected at least 4 total alias mappings (2 bidirectional aliases), got %d", totalMappings)
	}
}
