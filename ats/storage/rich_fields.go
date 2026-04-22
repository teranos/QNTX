package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// RichFieldInfo contains detailed information about a rich string field
// declared by one or more type attestations.
type RichFieldInfo struct {
	Field       string   `json:"field"`
	Count       int      `json:"count"`        // Attestations with a non-empty value for this field
	SourceTypes []string `json:"source_types"` // Type definitions that declare this field
}

// Cache TTL for type definitions
const typeFieldsCacheTTL = 5 * time.Minute

// getTypeDefinitions queries type definition attestations and extracts RichStringFields.
// Returns a map of type name -> list of rich string fields. Cached for performance.
func (bs *BoundedStore) getTypeDefinitions(ctx context.Context) (map[string][]string, error) {
	bs.typeFieldsCacheLock.RLock()
	if bs.typeFieldsCache != nil && time.Since(bs.typeFieldsCacheTime) < typeFieldsCacheTTL {
		defer bs.typeFieldsCacheLock.RUnlock()
		return bs.typeFieldsCache, nil
	}
	bs.typeFieldsCacheLock.RUnlock()

	rows, err := bs.db.QueryContext(ctx, `
		SELECT json_extract(subjects, '$[0]') as type_name, attributes
		FROM attestations
		WHERE json_extract(predicates, '$[0]') = 'type'
		ORDER BY created_at DESC
		LIMIT 1000
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query type definitions")
	}
	defer rows.Close()

	typeFields := make(map[string][]string)

	for rows.Next() {
		var typeName string
		var attributesJSON string
		if err := rows.Scan(&typeName, &attributesJSON); err != nil {
			continue
		}

		var attrMap map[string]interface{}
		if err := json.Unmarshal([]byte(attributesJSON), &attrMap); err != nil {
			continue
		}

		var def types.TypeDef
		attrs.Scan(attrMap, &def)
		if len(def.RichStringFields) > 0 {
			typeFields[typeName] = def.RichStringFields
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate type definitions")
	}

	bs.typeFieldsCacheLock.Lock()
	bs.typeFieldsCache = typeFields
	bs.typeFieldsCacheTime = time.Now()
	bs.typeFieldsCacheLock.Unlock()

	return typeFields, nil
}

// GetDiscoveredRichFields returns the list of rich string fields declared by
// type attestations — the fields plugins should consider when indexing.
func (bs *BoundedStore) GetDiscoveredRichFields() []string {
	return bs.buildDynamicRichStringFields(context.Background())
}

// GetRichFieldsWithStats returns per-field usage counts and source type lists.
func (bs *BoundedStore) GetRichFieldsWithStats() ([]RichFieldInfo, error) {
	ctx := context.Background()

	typeFields, err := bs.getTypeDefinitions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get type definitions")
	}

	fieldSources := make(map[string][]string)
	for typeName, fields := range typeFields {
		for _, field := range fields {
			fieldSources[field] = append(fieldSources[field], typeName)
		}
	}

	if len(fieldSources) == 0 {
		return []RichFieldInfo{}, nil
	}

	result := make([]RichFieldInfo, 0, len(fieldSources))
	for field, sources := range fieldSources {
		var count int
		query := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM attestations
			WHERE json_extract(attributes, '$.%s') IS NOT NULL
			  AND json_extract(attributes, '$.%s') != ''
			  AND json_extract(attributes, '$.%s') != 'null'
		`, field, field, field)

		if err := bs.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			if bs.logger != nil {
				bs.logger.Debugw("Failed to count field usage", "field", field, "error", err)
			}
			count = 0
		}

		sort.Strings(sources)

		result = append(result, RichFieldInfo{
			Field:       field,
			Count:       count,
			SourceTypes: sources,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Field < result[j].Field
	})

	return result, nil
}

// buildDynamicRichStringFields aggregates rich string fields across all
// type definitions. Returns an empty slice when no type declares any.
func (bs *BoundedStore) buildDynamicRichStringFields(ctx context.Context) []string {
	typeFields, err := bs.getTypeDefinitions(ctx)
	if err != nil {
		if bs.logger != nil {
			bs.logger.Warnw("Failed to query type definitions, no fields available", "error", err)
		}
		return []string{}
	}

	fieldSet := make(map[string]bool)
	for _, fields := range typeFields {
		for _, field := range fields {
			fieldSet[field] = true
		}
	}

	result := make([]string, 0, len(fieldSet))
	for field := range fieldSet {
		result = append(result, field)
	}
	sort.Strings(result)

	return result
}
