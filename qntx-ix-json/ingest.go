package qntxixjson

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teranos/QNTX/ats/types"
)

// pollAndIngest fetches from the API and creates attestations based on configured mapping.
func (p *Plugin) pollAndIngest(ctx context.Context) error {
	p.mu.RLock()
	apiURL := p.apiURL
	authToken := p.authToken
	mode := p.mode
	p.mu.RUnlock()

	// TODO: pollAndIngest needs to be per-glyph once scheduling is per-glyph
	var mapping *MappingConfig

	if mode != ModeActiveRunning {
		return fmt.Errorf("cannot poll in mode %s", mode)
	}

	if apiURL == "" {
		return fmt.Errorf("API URL not configured")
	}

	if mapping == nil {
		return fmt.Errorf("mapping configuration not set")
	}

	// Fetch JSON from API
	data, err := p.fetchJSON(ctx, apiURL, authToken)
	if err != nil {
		return fmt.Errorf("failed to fetch from API: %w", err)
	}

	// Parse JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Create attestations
	store := p.services.ATSStore()
	if store == nil {
		return fmt.Errorf("ATSStore not available")
	}

	// Determine if response is array or single object
	switch v := jsonData.(type) {
	case []interface{}:
		// Array of objects - create one attestation per item
		for i, item := range v {
			if err := p.createAttestationFromJSON(ctx, store, item, mapping); err != nil {
				p.services.Logger("ix-json").Errorw("Failed to create attestation",
					"index", i,
					"error", err,
				)
			}
		}
		p.services.Logger("ix-json").Infow("Poll completed", "attestations_created", len(v))

	case map[string]interface{}:
		// Single object - create one attestation
		if err := p.createAttestationFromJSON(ctx, store, v, mapping); err != nil {
			return fmt.Errorf("failed to create attestation: %w", err)
		}
		p.services.Logger("ix-json").Infow("Poll completed", "attestations_created", 1)

	default:
		return fmt.Errorf("unexpected JSON type: %T", jsonData)
	}

	return nil
}

// createAttestationFromJSON creates a single attestation from a JSON object.
func (p *Plugin) createAttestationFromJSON(ctx context.Context, store interface{}, data interface{}, mapping *MappingConfig) error {
	obj, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected JSON object, got %T", data)
	}

	// Apply key remapping if configured
	if len(mapping.KeyRemapping) > 0 {
		remapped := make(map[string]interface{})
		for oldKey, val := range obj {
			newKey := mapping.KeyRemapping[oldKey]
			if newKey == "" {
				newKey = oldKey // Keep original if no mapping
			}
			remapped[newKey] = val
		}
		obj = remapped
	}

	// Extract SPC from configured paths
	subject := extractValue(obj, mapping.SubjectPath)
	predicate := extractValue(obj, mapping.PredicatePath)
	contextVal := extractValue(obj, mapping.ContextPath)

	if subject == "" || predicate == "" {
		return fmt.Errorf("subject or predicate missing from JSON (subject=%s, predicate=%s)", subject, predicate)
	}

	// Build attributes from remaining fields
	attributes := make(map[string]interface{})
	for k, v := range obj {
		// Skip SPC fields
		if k == mapping.SubjectPath || k == mapping.PredicatePath || k == mapping.ContextPath {
			continue
		}
		attributes[k] = v
	}

	// Build contexts array
	contexts := []string{}
	if contextVal != "" {
		contexts = append(contexts, contextVal)
	}

	// Create attestation using AsCommand
	cmd := &types.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   contexts,
		Attributes: attributes,
		Source:     "ix-json",
	}

	// Type assertion to get the correct store interface
	atsStore, ok := store.(interface {
		GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error)
	})
	if !ok {
		return fmt.Errorf("ATSStore does not support GenerateAndCreateAttestation")
	}

	if _, err := atsStore.GenerateAndCreateAttestation(ctx, cmd); err != nil {
		return fmt.Errorf("failed to create attestation (subject=%s, predicate=%s): %w", subject, predicate, err)
	}

	p.services.Logger("ix-json").Debugw("Attestation created",
		"subject", subject,
		"predicate", predicate,
		"context", contextVal,
	)

	return nil
}

// extractValue extracts a value from a JSON object using a simple path (e.g., "id" or "user.name").
func extractValue(obj map[string]interface{}, path string) string {
	if path == "" {
		return ""
	}

	// Simple path traversal (supports "field" or "field.subfield")
	parts := splitPath(path)
	current := interface{}(obj)

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = m[part]
	}

	// Convert to string
	switch v := current.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// splitPath splits a path like "user.name" into ["user", "name"].
func splitPath(path string) []string {
	result := []string{}
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			if i > start {
				result = append(result, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		result = append(result, path[start:])
	}
	return result
}

// inferMapping attempts to infer a reasonable default mapping from JSON structure.
func (p *Plugin) inferMapping(data []byte) *MappingConfig {
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return &MappingConfig{}
	}

	// Extract first object for analysis
	var obj map[string]interface{}
	switch v := jsonData.(type) {
	case []interface{}:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]interface{}); ok {
				obj = m
			}
		}
	case map[string]interface{}:
		obj = v
	}

	if obj == nil {
		return &MappingConfig{}
	}

	// Heuristics for SPC fields:
	// - Subject: "id", "name", "title", first string field
	// - Predicate: "type", "kind", "event", second string field
	// - Context: "source", "origin", "domain", third string field
	subjectPath := inferField(obj, []string{"id", "name", "title"})
	predicatePath := inferField(obj, []string{"type", "kind", "event", "action"})
	contextPath := inferField(obj, []string{"source", "origin", "domain", "context"})

	p.services.Logger("ix-json").Infow("Inferred mapping",
		"subject", subjectPath,
		"predicate", predicatePath,
		"context", contextPath,
	)

	return &MappingConfig{
		SubjectPath:   subjectPath,
		PredicatePath: predicatePath,
		ContextPath:   contextPath,
		RichFields:    []string{},
		KeyRemapping:  make(map[string]string),
	}
}

// inferField finds the first matching field from candidates, or returns first string field.
func inferField(obj map[string]interface{}, candidates []string) string {
	// Try candidates first
	for _, candidate := range candidates {
		if _, exists := obj[candidate]; exists {
			return candidate
		}
	}

	// Fallback: return first string field
	for k, v := range obj {
		if _, ok := v.(string); ok {
			return k
		}
	}

	// Ultimate fallback: return first key
	for k := range obj {
		return k
	}

	return ""
}
