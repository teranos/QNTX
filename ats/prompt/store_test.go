package prompt

import (
	"context"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestSavePrompt_CreatesAttestation(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:     "test-prompt",
		Template: "Hello {{subject}}",
	}

	saved, err := store.SavePrompt(ctx, prompt, "test-actor")
	if err != nil {
		t.Fatalf("SavePrompt failed: %v", err)
	}

	if saved.ID == "" {
		t.Error("expected non-empty ID")
	}
	if saved.Name != "test-prompt" {
		t.Errorf("expected name 'test-prompt', got '%s'", saved.Name)
	}
	if saved.Template != "Hello {{subject}}" {
		t.Errorf("expected template 'Hello {{subject}}', got '%s'", saved.Template)
	}
	if saved.Version != 1 {
		t.Errorf("expected version 1, got %d", saved.Version)
	}
	if saved.CreatedBy != "test-actor" {
		t.Errorf("expected actor 'test-actor', got '%s'", saved.CreatedBy)
	}
}

func TestSavePrompt_VersionIncrementsOnUpdate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Create first version
	prompt1 := &StoredPrompt{
		Name:     "versioned-prompt",
		Template: "Version 1: {{subject}}",
	}
	saved1, err := store.SavePrompt(ctx, prompt1, "actor")
	if err != nil {
		t.Fatalf("SavePrompt v1 failed: %v", err)
	}
	if saved1.Version != 1 {
		t.Errorf("expected version 1, got %d", saved1.Version)
	}

	// Create second version with same name
	prompt2 := &StoredPrompt{
		Name:     "versioned-prompt",
		Template: "Version 2: {{subject}} {{predicate}}",
	}
	saved2, err := store.SavePrompt(ctx, prompt2, "actor")
	if err != nil {
		t.Fatalf("SavePrompt v2 failed: %v", err)
	}
	if saved2.Version != 2 {
		t.Errorf("expected version 2, got %d", saved2.Version)
	}

	// Create third version
	prompt3 := &StoredPrompt{
		Name:     "versioned-prompt",
		Template: "Version 3: {{subjects}}",
	}
	saved3, err := store.SavePrompt(ctx, prompt3, "actor")
	if err != nil {
		t.Fatalf("SavePrompt v3 failed: %v", err)
	}
	if saved3.Version != 3 {
		t.Errorf("expected version 3, got %d", saved3.Version)
	}

	// Verify GetPromptByName returns latest
	latest, err := store.GetPromptByName(ctx, "versioned-prompt")
	if err != nil {
		t.Fatalf("GetPromptByName failed: %v", err)
	}
	if latest.Version != 3 {
		t.Errorf("expected latest version 3, got %d", latest.Version)
	}
	if latest.Template != "Version 3: {{subjects}}" {
		t.Errorf("expected latest template, got '%s'", latest.Template)
	}
}

func TestGetPromptVersions_ReturnsAllVersions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Create multiple versions
	for i := 1; i <= 3; i++ {
		prompt := &StoredPrompt{
			Name:     "multi-version",
			Template: "Template v{{subject}}",
		}
		_, err := store.SavePrompt(ctx, prompt, "actor")
		if err != nil {
			t.Fatalf("SavePrompt v%d failed: %v", i, err)
		}
	}

	versions, err := store.GetPromptVersions(ctx, "multi-version", 10)
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

func TestListPrompts_ReturnsAllPrompts(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Create different prompts
	prompts := []string{"prompt-a", "prompt-b", "prompt-c"}
	for _, name := range prompts {
		prompt := &StoredPrompt{
			Name:     name,
			Template: "Hello {{subject}}",
		}
		_, err := store.SavePrompt(ctx, prompt, "actor")
		if err != nil {
			t.Fatalf("SavePrompt '%s' failed: %v", name, err)
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

func TestSavePrompt_StoresOptionalFields(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:         "full-prompt",
		Template:     "Analyze {{subject}}",
		SystemPrompt: "You are a helpful assistant",
		AxPattern:    "ALICE speaks english",
		Provider:     "openrouter",
		Model:        "gpt-4",
	}

	_, err := store.SavePrompt(ctx, prompt, "actor")
	if err != nil {
		t.Fatalf("SavePrompt failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := store.GetPromptByName(ctx, "full-prompt")
	if err != nil {
		t.Fatalf("GetPromptByName failed: %v", err)
	}

	if retrieved.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("expected system prompt preserved, got '%s'", retrieved.SystemPrompt)
	}
	if retrieved.AxPattern != "ALICE speaks english" {
		t.Errorf("expected ax pattern preserved, got '%s'", retrieved.AxPattern)
	}
	if retrieved.Provider != "openrouter" {
		t.Errorf("expected provider preserved, got '%s'", retrieved.Provider)
	}
	if retrieved.Model != "gpt-4" {
		t.Errorf("expected model preserved, got '%s'", retrieved.Model)
	}
}

func TestSavePrompt_ValidatesTemplate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	// Invalid template with unknown field
	prompt := &StoredPrompt{
		Name:     "invalid-template",
		Template: "Hello {{unknownfield}}",
	}

	_, err := store.SavePrompt(ctx, prompt, "actor")
	if err == nil {
		t.Error("expected error for invalid template field, got nil")
	}
}

func TestSavePrompt_RequiresName(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:     "",
		Template: "Hello {{subject}}",
	}

	_, err := store.SavePrompt(ctx, prompt, "actor")
	if err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestSavePrompt_RequiresTemplate(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewPromptStore(db)
	ctx := context.Background()

	prompt := &StoredPrompt{
		Name:     "no-template",
		Template: "",
	}

	_, err := store.SavePrompt(ctx, prompt, "actor")
	if err == nil {
		t.Error("expected error for empty template, got nil")
	}
}
