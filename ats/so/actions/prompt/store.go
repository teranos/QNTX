package prompt

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

// StoredPrompt represents a prompt stored as an attestation
type StoredPrompt struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Filename     string    `json:"filename"`        // Source file (used as context)
	Template     string    `json:"template"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	AxPattern    string    `json:"ax_pattern,omitempty"` // Optional linked ax query
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	Version      int       `json:"version"`
}

// PromptStore handles prompt persistence as attestations
type PromptStore struct {
	db    *sql.DB
	store *storage.SQLStore
}

// NewPromptStore creates a new prompt store
func NewPromptStore(db *sql.DB) *PromptStore {
	return &PromptStore{
		db:    db,
		store: storage.NewSQLStore(db, nil),
	}
}

// Predicates used for prompt attestations
const (
	PredicatePromptTemplate = "prompt-template"
	PredicatePromptVersion  = "prompt-version"
	ContextPromptLibrary    = "prompt-library"
)

// SavePrompt stores a prompt as an attestation
// If a prompt with the same name and filename exists, a new version is created
func (ps *PromptStore) SavePrompt(ctx context.Context, prompt *StoredPrompt, actor string) (*StoredPrompt, error) {
	if prompt.Name == "" {
		return nil, errors.New("prompt name is required")
	}
	if prompt.Filename == "" {
		return nil, errors.New("prompt filename is required")
	}
	if prompt.Template == "" {
		return nil, errors.New("prompt template is required")
	}

	// Validate template syntax
	if err := ValidateTemplate(prompt.Template); err != nil {
		return nil, errors.Wrap(err, "invalid template")
	}

	// Get current version if exists (by filename)
	existing, err := ps.GetPromptByFilename(ctx, prompt.Filename)
	version := 1
	if err == nil && existing != nil {
		version = existing.Version + 1
	}

	// Generate ASID for the prompt using filename as context
	asid, err := id.GenerateASID(prompt.Name, PredicatePromptTemplate, prompt.Filename, actor)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate prompt ID")
	}

	now := time.Now()

	// Build attributes
	attrs := map[string]interface{}{
		"template": prompt.Template,
		"version":  version,
	}
	if prompt.SystemPrompt != "" {
		attrs["system_prompt"] = prompt.SystemPrompt
	}
	if prompt.AxPattern != "" {
		attrs["ax_pattern"] = prompt.AxPattern
	}
	if prompt.Provider != "" {
		attrs["provider"] = prompt.Provider
	}
	if prompt.Model != "" {
		attrs["model"] = prompt.Model
	}

	// Create the attestation
	as := &types.As{
		ID:         asid,
		Subjects:   []string{prompt.Name},
		Predicates: []string{PredicatePromptTemplate},
		Contexts:   []string{prompt.Filename},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt-editor",
		Attributes: attrs,
		CreatedAt:  now,
	}

	if err := ps.store.CreateAttestation(as); err != nil {
		return nil, errors.Wrap(err, "failed to store prompt")
	}

	return &StoredPrompt{
		ID:           asid,
		Name:         prompt.Name,
		Filename:     prompt.Filename,
		Template:     prompt.Template,
		SystemPrompt: prompt.SystemPrompt,
		AxPattern:    prompt.AxPattern,
		Provider:     prompt.Provider,
		Model:        prompt.Model,
		CreatedBy:    actor,
		CreatedAt:    now,
		Version:      version,
	}, nil
}

// GetPromptByFilename returns the latest version of a prompt by filename
func (ps *PromptStore) GetPromptByFilename(ctx context.Context, filename string) (*StoredPrompt, error) {
	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, attributes
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		  AND EXISTS (SELECT 1 FROM json_each(contexts) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var (
		asID           string
		subjectsJSON   string
		predicatesJSON string
		contextsJSON   string
		actorsJSON     string
		timestamp      time.Time
		attributesJSON string
	)

	err := ps.db.QueryRowContext(ctx, query, PredicatePromptTemplate, filename).Scan(
		&asID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timestamp, &attributesJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt by filename")
	}

	return ps.parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON, timestamp, attributesJSON)
}

// GetPromptByName returns the latest version of a prompt by name
// Note: Since prompts are now keyed by filename, this searches across all files
func (ps *PromptStore) GetPromptByName(ctx context.Context, name string) (*StoredPrompt, error) {
	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, attributes
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(subjects) WHERE value = ?)
		  AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var (
		asID           string
		subjectsJSON   string
		predicatesJSON string
		contextsJSON   string
		actorsJSON     string
		timestamp      time.Time
		attributesJSON string
	)

	err := ps.db.QueryRowContext(ctx, query, name, PredicatePromptTemplate).Scan(
		&asID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timestamp, &attributesJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt by name")
	}

	return ps.parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON, timestamp, attributesJSON)
}

// GetPromptByID returns a specific prompt by ID
func (ps *PromptStore) GetPromptByID(ctx context.Context, promptID string) (*StoredPrompt, error) {
	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, attributes
		FROM attestations
		WHERE id = ?
	`

	var (
		asID           string
		subjectsJSON   string
		predicatesJSON string
		contextsJSON   string
		actorsJSON     string
		timestamp      time.Time
		attributesJSON string
	)

	err := ps.db.QueryRowContext(ctx, query, promptID).Scan(
		&asID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timestamp, &attributesJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt")
	}

	return ps.parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON, timestamp, attributesJSON)
}

// ListPrompts returns all prompts, most recent first
func (ps *PromptStore) ListPrompts(ctx context.Context, limit int) ([]*StoredPrompt, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, attributes
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := ps.db.QueryContext(ctx, query, PredicatePromptTemplate, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list prompts")
	}
	defer rows.Close()

	var prompts []*StoredPrompt
	for rows.Next() {
		var (
			asID           string
			subjectsJSON   string
			predicatesJSON string
			contextsJSON   string
			actorsJSON     string
			timestamp      time.Time
			attributesJSON string
		)

		if err := rows.Scan(&asID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timestamp, &attributesJSON); err != nil {
			return nil, errors.Wrap(err, "failed to scan prompt row")
		}

		prompt, err := ps.parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON, timestamp, attributesJSON)
		if err != nil {
			continue // Skip malformed entries
		}
		prompts = append(prompts, prompt)
	}

	return prompts, nil
}

// GetPromptVersions returns all versions of a prompt by filename
func (ps *PromptStore) GetPromptVersions(ctx context.Context, filename string, limit int) ([]*StoredPrompt, error) {
	if limit <= 0 {
		limit = 16 // Bounded storage default
	}

	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, attributes
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		  AND EXISTS (SELECT 1 FROM json_each(contexts) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := ps.db.QueryContext(ctx, query, PredicatePromptTemplate, filename, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list prompt versions")
	}
	defer rows.Close()

	var prompts []*StoredPrompt
	for rows.Next() {
		var (
			asID           string
			subjectsJSON   string
			predicatesJSON string
			contextsJSON   string
			actorsJSON     string
			timestamp      time.Time
			attributesJSON string
		)

		if err := rows.Scan(&asID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timestamp, &attributesJSON); err != nil {
			return nil, errors.Wrap(err, "failed to scan prompt row")
		}

		prompt, err := ps.parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON, timestamp, attributesJSON)
		if err != nil {
			continue
		}
		prompts = append(prompts, prompt)
	}

	return prompts, nil
}

// parsePromptFromRow converts database row data into a StoredPrompt
func (ps *PromptStore) parsePromptFromRow(asID, subjectsJSON, contextsJSON, actorsJSON string, timestamp time.Time, attributesJSON string) (*StoredPrompt, error) {
	var subjects []string
	if err := json.Unmarshal([]byte(subjectsJSON), &subjects); err != nil {
		return nil, errors.Wrap(err, "failed to parse subjects")
	}

	var contexts []string
	if err := json.Unmarshal([]byte(contextsJSON), &contexts); err != nil {
		return nil, errors.Wrap(err, "failed to parse contexts")
	}

	var actors []string
	if err := json.Unmarshal([]byte(actorsJSON), &actors); err != nil {
		return nil, errors.Wrap(err, "failed to parse actors")
	}

	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(attributesJSON), &attrs); err != nil {
		return nil, errors.Wrap(err, "failed to parse attributes")
	}

	name := ""
	if len(subjects) > 0 {
		name = subjects[0]
	}

	filename := ""
	if len(contexts) > 0 {
		filename = contexts[0]
	}

	createdBy := ""
	if len(actors) > 0 {
		createdBy = actors[0]
	}

	prompt := &StoredPrompt{
		ID:        asID,
		Name:      name,
		Filename:  filename,
		CreatedBy: createdBy,
		CreatedAt: timestamp,
	}

	// Extract attributes
	if template, ok := attrs["template"].(string); ok {
		prompt.Template = template
	}
	if systemPrompt, ok := attrs["system_prompt"].(string); ok {
		prompt.SystemPrompt = systemPrompt
	}
	if axPattern, ok := attrs["ax_pattern"].(string); ok {
		prompt.AxPattern = axPattern
	}
	if provider, ok := attrs["provider"].(string); ok {
		prompt.Provider = provider
	}
	if model, ok := attrs["model"].(string); ok {
		prompt.Model = model
	}
	if version, ok := attrs["version"].(float64); ok {
		prompt.Version = int(version)
	}

	return prompt, nil
}
