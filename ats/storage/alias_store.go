package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AliasStore handles simple alias mappings
type AliasStore struct {
	db *sql.DB
}

// NewAliasStore creates a new alias storage instance
func NewAliasStore(db *sql.DB) *AliasStore {
	return &AliasStore{
		db: db,
	}
}

// CreateAlias creates a simple bidirectional alias (implements ats.AliasResolver)
// Aliases use case-insensitive lookups but preserve original values
func (as *AliasStore) CreateAlias(alias, target, createdBy string) error {
	// Validate inputs to prevent empty or meaningless aliases
	if alias == "" {
		return fmt.Errorf("alias cannot be empty")
	}
	if target == "" {
		return fmt.Errorf("target cannot be empty")
	}
	if strings.EqualFold(alias, target) {
		return fmt.Errorf("alias and target cannot be identical")
	}

	// Use transaction to ensure atomicity of bidirectional alias creation
	ctx := context.Background()
	tx, err := as.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	now := time.Now()

	// Insert both directions preserving original values
	// Lookups use COLLATE NOCASE for case-insensitive matching
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO aliases (alias, target, created_by, created_at)
		VALUES (?, ?, ?, ?)`,
		alias, target, createdBy, now)
	if err != nil {
		return fmt.Errorf("failed to create alias %s -> %s: %w", alias, target, err)
	}

	// Create reverse mapping
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO aliases (alias, target, created_by, created_at)
		VALUES (?, ?, ?, ?)`,
		target, alias, createdBy, now)
	if err != nil {
		return fmt.Errorf("failed to create reverse alias %s -> %s: %w", target, alias, err)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ResolveAlias returns all identifiers that should be included when searching for the given identifier (implements ats.AliasResolver)
// Uses case-insensitive matching via COLLATE NOCASE
func (as *AliasStore) ResolveAlias(identifier string) ([]string, error) {
	ctx := context.Background()

	query := `
		SELECT target
		FROM aliases
		WHERE alias = ? COLLATE NOCASE
		UNION
		SELECT ?`

	rows, err := as.db.QueryContext(ctx, query, identifier, identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve alias for %s: %w", identifier, err)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan identifier: %w", err)
		}
		identifiers = append(identifiers, id)
	}

	// Remove duplicates and return
	return removeDuplicates(identifiers), rows.Err()
}

// GetAllAliases returns all alias mappings (implements ats.AliasResolver)
func (as *AliasStore) GetAllAliases() (map[string][]string, error) {
	query := `SELECT alias, target FROM aliases ORDER BY alias`

	ctx := context.Background()
	rows, err := as.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all aliases: %w", err)
	}
	defer rows.Close()

	aliases := make(map[string][]string)
	for rows.Next() {
		var alias, target string
		if err := rows.Scan(&alias, &target); err != nil {
			return nil, fmt.Errorf("failed to scan alias: %w", err)
		}
		aliases[alias] = append(aliases[alias], target)
	}

	return aliases, rows.Err()
}

// RemoveAlias removes an alias mapping (implements ats.AliasResolver)
// Uses case-insensitive matching via COLLATE NOCASE
func (as *AliasStore) RemoveAlias(alias, target string) error {
	// Validate inputs
	if alias == "" {
		return fmt.Errorf("alias cannot be empty")
	}
	if target == "" {
		return fmt.Errorf("target cannot be empty")
	}

	// Remove both directions with case-insensitive matching
	ctx := context.Background()
	_, err := as.db.ExecContext(ctx, `
		DELETE FROM aliases
		WHERE (alias = ? COLLATE NOCASE AND target = ? COLLATE NOCASE)
		   OR (alias = ? COLLATE NOCASE AND target = ? COLLATE NOCASE)`,
		alias, target, target, alias)
	if err != nil {
		return fmt.Errorf("failed to remove alias between %s and %s: %w", alias, target, err)
	}
	return nil
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}
