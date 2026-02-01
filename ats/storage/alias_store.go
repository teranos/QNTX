package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/teranos/QNTX/errors"
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
func (as *AliasStore) CreateAlias(ctx context.Context, alias, target, createdBy string) error {
	// Validate inputs to prevent empty or meaningless aliases
	if alias == "" {
		return errors.New("alias cannot be empty")
	}
	if target == "" {
		return errors.New("target cannot be empty")
	}
	if strings.EqualFold(alias, target) {
		return errors.New("alias and target cannot be identical")
	}

	// Use transaction to ensure atomicity of bidirectional alias creation
	tx, err := as.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
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
		return errors.Wrapf(err, "failed to create alias %s -> %s", alias, target)
	}

	// Create reverse mapping
	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO aliases (alias, target, created_by, created_at)
		VALUES (?, ?, ?, ?)`,
		target, alias, createdBy, now)
	if err != nil {
		return errors.Wrapf(err, "failed to create reverse alias %s -> %s", target, alias)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	return nil
}

// ResolveAlias returns all identifiers that should be included when searching for the given identifier (implements ats.AliasResolver)
// Uses case-insensitive matching via COLLATE NOCASE
func (as *AliasStore) ResolveAlias(ctx context.Context, identifier string) ([]string, error) {
	query := `
		SELECT target
		FROM aliases
		WHERE alias = ? COLLATE NOCASE
		UNION
		SELECT ?`

	rows, err := as.db.QueryContext(ctx, query, identifier, identifier)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve alias for %s", identifier)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrap(err, "failed to scan identifier")
		}
		identifiers = append(identifiers, id)
	}

	// Remove duplicates and return
	return removeDuplicates(identifiers), rows.Err()
}

// GetAllAliases returns all alias mappings (implements ats.AliasResolver)
func (as *AliasStore) GetAllAliases(ctx context.Context) (map[string][]string, error) {
	query := `SELECT alias, target FROM aliases ORDER BY alias`

	rows, err := as.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get all aliases")
	}
	defer rows.Close()

	aliases := make(map[string][]string)
	for rows.Next() {
		var alias, target string
		if err := rows.Scan(&alias, &target); err != nil {
			return nil, errors.Wrap(err, "failed to scan alias")
		}
		aliases[alias] = append(aliases[alias], target)
	}

	return aliases, rows.Err()
}

// RemoveAlias removes an alias mapping (implements ats.AliasResolver)
// Uses case-insensitive matching via COLLATE NOCASE
func (as *AliasStore) RemoveAlias(ctx context.Context, alias, target string) error {
	// Validate inputs
	if alias == "" {
		return errors.New("alias cannot be empty")
	}
	if target == "" {
		return errors.New("target cannot be empty")
	}

	// Remove both directions with case-insensitive matching
	_, err := as.db.ExecContext(ctx, `
		DELETE FROM aliases
		WHERE (alias = ? COLLATE NOCASE AND target = ? COLLATE NOCASE)
		   OR (alias = ? COLLATE NOCASE AND target = ? COLLATE NOCASE)`,
		alias, target, target, alias)
	if err != nil {
		return errors.Wrapf(err, "failed to remove alias between %s and %s", alias, target)
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
