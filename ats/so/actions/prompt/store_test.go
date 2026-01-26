package prompt

import (
	"context"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// These tests demonstrate storing recipe prompts that combine kitchen inventory
// attestations into LLM prompts for cooking suggestions.
//
// Inventory attestations follow the pattern:
//   ALICE is inventory of fridge by smartfridge_001 at 2024-06-15
//     ATTRIBUTES{milk:240ml, eggs:6pc, butter:100g}
//   ALICE is inventory of cupboard by manual_entry at 2024-06-15
//     ATTRIBUTES{rigatoni:250g, canned_tomatoes:2pc, onion:3pc}

func TestSavePrompt_RecipeGenerator(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// A prompt that generates recipes from inventory attestations
	prompt := &StoredPrompt{
		Name:     "dinner-suggestions",
		Filename: "dinner-suggestions.md",
		Template: "Based on {{subject}}'s available ingredients from {{context}}: {{attributes}}, suggest 3 dinner recipes.",
	}

	saved, err := store.SavePrompt(ctx, prompt, "home-chef-app")
	if err != nil {
		t.Fatalf("SavePrompt failed: %v", err)
	}

	if saved.ID == "" {
		t.Error("expected non-empty ID")
	}
	if saved.Name != "dinner-suggestions" {
		t.Errorf("expected name 'dinner-suggestions', got '%s'", saved.Name)
	}
	if saved.Version != 1 {
		t.Errorf("expected version 1, got %d", saved.Version)
	}
	if saved.CreatedBy != "home-chef-app" {
		t.Errorf("expected actor 'home-chef-app', got '%s'", saved.CreatedBy)
	}
}

func TestSavePrompt_VersionIncrementsAsRecipeEvolves(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Version 1: Simple recipe suggestion
	v1 := &StoredPrompt{
		Name:     "weekly-meal-planner",
		Filename: "weekly-meal-planner.md",
		Template: "Suggest meals for {{subject}} using {{attributes}}",
	}
	saved1, err := store.SavePrompt(ctx, v1, "meal-planner")
	if err != nil {
		t.Fatalf("SavePrompt v1 failed: %v", err)
	}
	if saved1.Version != 1 {
		t.Errorf("expected version 1, got %d", saved1.Version)
	}

	// Version 2: Added dietary context
	v2 := &StoredPrompt{
		Name:     "weekly-meal-planner",
		Filename: "weekly-meal-planner.md",
		Template: "Plan {{subject}}'s weekly meals using {{attributes}} from {{context}}. Consider freshness dates.",
	}
	saved2, err := store.SavePrompt(ctx, v2, "meal-planner")
	if err != nil {
		t.Fatalf("SavePrompt v2 failed: %v", err)
	}
	if saved2.Version != 2 {
		t.Errorf("expected version 2, got %d", saved2.Version)
	}

	// Version 3: Added multi-source awareness
	v3 := &StoredPrompt{
		Name:     "weekly-meal-planner",
		Filename: "weekly-meal-planner.md",
		Template: "Plan {{subject}}'s meals using inventory from {{contexts}} (sources: {{actors}}). Items: {{attributes}}. Updated: {{temporal}}",
	}
	saved3, err := store.SavePrompt(ctx, v3, "meal-planner")
	if err != nil {
		t.Fatalf("SavePrompt v3 failed: %v", err)
	}
	if saved3.Version != 3 {
		t.Errorf("expected version 3, got %d", saved3.Version)
	}

	// Verify latest version is returned
	latest, err := store.GetPromptByName(ctx, "weekly-meal-planner")
	if err != nil {
		t.Fatalf("GetPromptByName failed: %v", err)
	}
	if latest.Version != 3 {
		t.Errorf("expected latest version 3, got %d", latest.Version)
	}
	if latest.Template != v3.Template {
		t.Errorf("expected latest template with multi-source awareness")
	}
}

func TestGetPromptVersions_RecipePromptHistory(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Create recipe prompt versions showing evolution
	templates := []string{
		"Suggest recipes using {{attributes}}",
		"Suggest {{subject}}'s dinner using {{attributes}} from {{context}}",
		"Create a shopping list for {{subject}} based on {{context}} inventory: {{attributes}}",
	}

	for _, tmpl := range templates {
		prompt := &StoredPrompt{
			Name:     "kitchen-assistant",
			Filename: "kitchen-assistant.md",
			Template: tmpl,
		}
		_, err := store.SavePrompt(ctx, prompt, "chef-bot")
		if err != nil {
			t.Fatalf("SavePrompt failed: %v", err)
		}
	}

	versions, err := store.GetPromptVersions(ctx, "kitchen-assistant.md", 10)
	if err != nil {
		t.Fatalf("GetPromptVersions failed: %v", err)
	}

	if len(versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(versions))
	}

	// Verify versions are in descending order (most recent first)
	for i, v := range versions {
		expectedVersion := 3 - i
		if v.Version != expectedVersion {
			t.Errorf("version[%d] expected version %d, got %d", i, expectedVersion, v.Version)
		}
	}
}

func TestListPrompts_MultipleRecipePrompts(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Create different recipe-related prompts
	prompts := []struct {
		name     string
		template string
	}{
		{"quick-dinner", "Quick dinner ideas using {{attributes.eggs}}, {{attributes.cheese}}"},
		{"leftover-magic", "Creative recipes to use up {{subject}}'s leftovers: {{attributes}}"},
		{"meal-prep-sunday", "Meal prep plan for {{subject}} using {{context}} inventory: {{attributes}}"},
	}

	for _, p := range prompts {
		prompt := &StoredPrompt{
			Name:     p.name,
			Filename: p.name + ".md",
			Template: p.template,
		}
		_, err := store.SavePrompt(ctx, prompt, "recipe-app")
		if err != nil {
			t.Fatalf("SavePrompt '%s' failed: %v", p.name, err)
		}
	}

	list, err := store.ListPrompts(ctx, 100)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("expected 3 prompts, got %d", len(list))
	}
}

func TestSavePrompt_FullRecipeConfiguration(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Full configuration for a recipe prompt with all options
	prompt := &StoredPrompt{
		Name:         "smart-chef",
		Filename:     "smart-chef.md",
		Template:     "Based on {{subject}}'s kitchen inventory from {{contexts}}, recorded by {{actors}} at {{temporal}}, suggest healthy dinner recipes using: {{attributes}}. Prioritize items expiring soon.",
		SystemPrompt: "You are a professional chef and nutritionist. Suggest balanced, healthy meals that minimize food waste. Consider cooking time and difficulty level.",
		AxPattern:    "* is inventory of fridge,cupboard by *",
		Provider:     "openrouter",
		Model:        "anthropic/claude-3-haiku",
	}

	_, err := store.SavePrompt(ctx, prompt, "smart-kitchen")
	if err != nil {
		t.Fatalf("SavePrompt failed: %v", err)
	}

	// Retrieve and verify all fields preserved
	retrieved, err := store.GetPromptByName(ctx, "smart-chef")
	if err != nil {
		t.Fatalf("GetPromptByName failed: %v", err)
	}

	if retrieved.SystemPrompt != prompt.SystemPrompt {
		t.Errorf("expected system prompt preserved")
	}
	if retrieved.AxPattern != "* is inventory of fridge,cupboard by *" {
		t.Errorf("expected ax pattern preserved, got '%s'", retrieved.AxPattern)
	}
	if retrieved.Provider != "openrouter" {
		t.Errorf("expected provider 'openrouter', got '%s'", retrieved.Provider)
	}
	if retrieved.Model != "anthropic/claude-3-haiku" {
		t.Errorf("expected model preserved, got '%s'", retrieved.Model)
	}
}

func TestSavePrompt_ValidatesRecipeTemplate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Invalid template with unknown field
	prompt := &StoredPrompt{
		Name:     "bad-recipe-prompt",
		Filename: "bad-recipe-prompt.md",
		Template: "Use {{ingredients}} to make dinner", // "ingredients" is not a valid field
	}

	_, err := store.SavePrompt(ctx, prompt, "tester")
	if err == nil {
		t.Error("expected error for invalid template field 'ingredients', got nil")
	}
}

func TestSavePrompt_RequiresName(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:     "",
		Filename: "test.md",
		Template: "Suggest recipes using {{attributes}}",
	}

	_, err := store.SavePrompt(ctx, prompt, "tester")
	if err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestSavePrompt_RequiresTemplate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:     "empty-recipe",
		Filename: "empty-recipe.md",
		Template: "",
	}

	_, err := store.SavePrompt(ctx, prompt, "tester")
	if err == nil {
		t.Error("expected error for empty template, got nil")
	}
}
