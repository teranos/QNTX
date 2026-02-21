package prompt

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

// StoredPrompt represents a prompt stored as an attestation.
// Fields sourced from attestation attributes carry `attr` tags.
type StoredPrompt struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Filename     string    `json:"filename"` // Source file (used as context)
	Template     string    `json:"template" attr:"template"`
	SystemPrompt string    `json:"system_prompt,omitempty" attr:"system_prompt,omitempty"`
	AxPattern    string    `json:"ax_pattern,omitempty" attr:"ax_pattern,omitempty"` // Optional linked ax query
	Provider     string    `json:"provider,omitempty" attr:"provider,omitempty"`
	Model        string    `json:"model,omitempty" attr:"model,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	Version      int       `json:"version" attr:"version"`
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
	// TODO(issue #344): Make filename optional for attestation-only prompts
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
	prompt.Version = version

	// Create the attestation
	as := &types.As{
		ID:         asid,
		Subjects:   []string{prompt.Name},
		Predicates: []string{PredicatePromptTemplate},
		Contexts:   []string{prompt.Filename},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt-editor",
		Attributes: attrs.From(prompt),
		CreatedAt:  now,
	}

	// TODO(issue #346): Pass ctx to CreateAttestation once storage layer supports context
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
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		  AND EXISTS (SELECT 1 FROM json_each(contexts) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT 1
	`

	rows, err := ps.db.QueryContext(ctx, query, PredicatePromptTemplate, filename)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt by filename")
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	as, err := storage.ScanAttestation(rows)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan attestation")
	}

	return ps.attestationToPrompt(as), nil
}

// GetPromptByName returns the latest version of a prompt by name
// Note: Since prompts are now keyed by filename, this searches across all files
func (ps *PromptStore) GetPromptByName(ctx context.Context, name string) (*StoredPrompt, error) {
	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		FROM attestations
		WHERE EXISTS (SELECT 1 FROM json_each(subjects) WHERE value = ?)
		  AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)
		ORDER BY timestamp DESC
		LIMIT 1
	`

	rows, err := ps.db.QueryContext(ctx, query, name, PredicatePromptTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt by name")
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	as, err := storage.ScanAttestation(rows)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan attestation")
	}

	return ps.attestationToPrompt(as), nil
}

// GetPromptByID returns a specific prompt by ID
func (ps *PromptStore) GetPromptByID(ctx context.Context, promptID string) (*StoredPrompt, error) {
	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		FROM attestations
		WHERE id = ?
	`

	rows, err := ps.db.QueryContext(ctx, query, promptID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query prompt")
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	as, err := storage.ScanAttestation(rows)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan attestation")
	}

	return ps.attestationToPrompt(as), nil
}

// ListPrompts returns all prompts, most recent first
func (ps *PromptStore) ListPrompts(ctx context.Context, limit int) ([]*StoredPrompt, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
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
		as, err := storage.ScanAttestation(rows)
		if err != nil {
			continue // Skip malformed entries
		}
		prompts = append(prompts, ps.attestationToPrompt(as))
	}

	return prompts, nil
}

// GetPromptVersions returns all versions of a prompt by filename
func (ps *PromptStore) GetPromptVersions(ctx context.Context, filename string, limit int) ([]*StoredPrompt, error) {
	if limit <= 0 {
		limit = 16 // Bounded storage default
	}

	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
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
		as, err := storage.ScanAttestation(rows)
		if err != nil {
			continue
		}
		prompts = append(prompts, ps.attestationToPrompt(as))
	}

	return prompts, nil
}

// attestationToPrompt converts an attestation into a StoredPrompt
func (ps *PromptStore) attestationToPrompt(as *types.As) *StoredPrompt {
	prompt := &StoredPrompt{
		ID:        as.ID,
		CreatedAt: as.Timestamp,
	}
	if len(as.Subjects) > 0 {
		prompt.Name = as.Subjects[0]
	}
	if len(as.Contexts) > 0 {
		prompt.Filename = as.Contexts[0]
	}
	if len(as.Actors) > 0 {
		prompt.CreatedBy = as.Actors[0]
	}
	attrs.Scan(as.Attributes, prompt)
	return prompt
}
