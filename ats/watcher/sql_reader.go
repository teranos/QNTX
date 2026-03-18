package watcher

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// SQLReader adapts *sql.DB to the AttestationReader interface.
// Used in tests and as a fallback when Rust FFI is not available.
type SQLReader struct {
	db *sql.DB
}

// NewSQLReader creates an AttestationReader backed by Go's *sql.DB.
func NewSQLReader(db *sql.DB) *SQLReader {
	return &SQLReader{db: db}
}

func (r *SQLReader) GetAttestation(id string) (*types.As, error) {
	query := storage.AttestationSelectQuery + " WHERE id = ?"
	rows, err := r.db.Query(query, id)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query attestation %s", id)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, errors.Newf("attestation %s not found", id)
	}

	as, err := storage.ScanAttestation(rows)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to scan attestation %s", id)
	}
	return as, nil
}

func (r *SQLReader) QueryAttestationsRaw(sqlQuery string, params []interface{}) ([]*types.As, error) {
	rows, err := r.db.Query(sqlQuery, params...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute raw query")
	}
	defer rows.Close()

	var attestations []*types.As
	for rows.Next() {
		as, err := storage.ScanAttestation(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan attestation row")
		}
		attestations = append(attestations, as)
	}
	return attestations, rows.Err()
}
