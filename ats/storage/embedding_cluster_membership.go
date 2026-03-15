//go:build cgo && rustembeddings

package storage

import "github.com/teranos/QNTX/errors"

// ClusterMembership maps an attestation source_id to its cluster identity.
type ClusterMembership struct {
	ClusterID int     `json:"cluster_id"`
	Label     *string `json:"label"`
}

// GetClusterMemberships returns cluster assignments for a set of attestation IDs.
// Attestations without embeddings or in noise (cluster_id = -1) are omitted.
func (s *EmbeddingStore) GetClusterMemberships(sourceIDs []string) (map[string]ClusterMembership, error) {
	if len(sourceIDs) == 0 {
		return map[string]ClusterMembership{}, nil
	}

	placeholders := make([]byte, 0, len(sourceIDs)*2)
	args := make([]any, len(sourceIDs))
	for i, id := range sourceIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	query := `SELECT e.source_id, e.cluster_id, c.label
		FROM embeddings e
		LEFT JOIN clusters c ON c.id = e.cluster_id
		WHERE e.source_id IN (` + string(placeholders) + `)
		AND e.cluster_id != -1`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query cluster memberships for %d source IDs", len(sourceIDs))
	}
	defer rows.Close()

	result := make(map[string]ClusterMembership, len(sourceIDs))
	for rows.Next() {
		var sourceID string
		var m ClusterMembership
		if err := rows.Scan(&sourceID, &m.ClusterID, &m.Label); err != nil {
			return nil, errors.Wrapf(err, "failed to scan cluster membership at row %d", len(result)+1)
		}
		result[sourceID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate cluster memberships (read %d)", len(result))
	}
	return result, nil
}
