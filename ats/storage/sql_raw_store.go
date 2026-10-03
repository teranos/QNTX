package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
)

// SQLRawStore is a RawAttestationStore over Go's *sql.DB: the attestations
// table and its junction tables, written as crates/ats-sqlite's
// put_attestation writes them, so the Go reads in retrieval.go read them the
// same. It is QuickDev's store; bounded enforcement and the rest of what the
// Rust store does beyond the row are not here.
type SQLRawStore struct{ db *sql.DB }

var (
	_ RawAttestationStore = (*SQLRawStore)(nil)
	_ QueryableStore      = (*SQLRawStore)(nil)
	_ BatchGetStore       = (*SQLRawStore)(nil)
)

// NewSQLRawStore keeps attestations in db, which db.Migrate has migrated.
func NewSQLRawStore(db *sql.DB) *SQLRawStore { return &SQLRawStore{db: db} }

// junctions are the tables a row's values are indexed in, as ats-sqlite fills them.
var junctions = []struct {
	table, column string
	values        func(*types.As) []string
}{
	{"attestation_actors", "actor", func(as *types.As) []string { return as.Actors }},
	{"attestation_contexts", "context", func(as *types.As) []string { return as.Contexts }},
	{"attestation_subjects", "subject", func(as *types.As) []string { return as.Subjects }},
	{"attestation_predicates", "predicate", func(as *types.As) []string { return as.Predicates }},
}

// CreateAttestation writes the row and its junction rows in one transaction.
// An id already kept is refused, as ats-sqlite refuses it.
func (s *SQLRawStore) CreateAttestation(as *types.As) (err error) {
	if as == nil {
		return errors.New("no attestation to create")
	}
	if s.AttestationExists(as.ID) {
		return errors.Newf("attestation %s already exists", as.ID)
	}
	columns := map[string]string{}
	for name, values := range map[string][]string{"subjects": as.Subjects, "predicates": as.Predicates, "contexts": as.Contexts, "actors": as.Actors} {
		// A Rust Vec is never null; none is [].
		if values == nil {
			values = []string{}
		}
		encoded, err := jsonText(values)
		if err != nil {
			return errors.Wrapf(err, "the %s of attestation %s did not encode", name, as.ID)
		}
		columns[name] = encoded
	}
	var attributes any
	if len(as.Attributes) > 0 {
		encoded, err := jsonText(as.Attributes)
		if err != nil {
			return errors.Wrapf(err, "the attributes of attestation %s did not encode", as.ID)
		}
		attributes = encoded
	}
	var signerDID any
	if as.SignerDID != "" {
		signerDID = as.SignerDID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return errors.Wrapf(err, "failed to begin writing attestation %s", as.ID)
	}
	defer func() {
		if err != nil {
			err = sqlclose.With(err, tx.Rollback(), "the transaction writing attestation "+as.ID)
		}
	}()
	if _, err = tx.Exec(
		"INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		as.ID, columns["subjects"], columns["predicates"], columns["contexts"], columns["actors"],
		rfc3339(as.Timestamp), as.Source, attributes, rfc3339(as.CreatedAt), as.Signature, signerDID,
	); err != nil {
		return errors.Wrapf(err, "failed to insert attestation %s", as.ID)
	}
	for _, j := range junctions {
		for _, value := range j.values(as) {
			if _, err = tx.Exec("INSERT INTO "+j.table+" (attestation_id, "+j.column+") VALUES (?, ?)", as.ID, value); err != nil {
				return errors.Wrapf(err, "failed to index attestation %s in %s", as.ID, j.table)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrapf(err, "failed to commit attestation %s", as.ID)
	}
	return nil
}

// GetAttestation is the attestation by id; a miss is errors.Is(err, ErrNotFound).
func (s *SQLRawStore) GetAttestation(id string) (*types.As, error) {
	return GetAttestationByID(s.db, id)
}

// AttestationExists reports whether id is kept. A failed read answers false,
// as a miss would.
func (s *SQLRawStore) AttestationExists(id string) bool {
	var one int
	return s.db.QueryRow("SELECT 1 FROM attestations WHERE id = ?", id).Scan(&one) == nil
}

// CountAttestations is how many attestations are kept.
func (s *SQLRawStore) CountAttestations() (int, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count attestations")
	}
	return count, nil
}

// GetAttestations is the attestations the filter matches.
func (s *SQLRawStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	return GetAttestations(s.db, filter)
}

// GetAttestationsByIDs is the attestations of these ids that are kept.
func (s *SQLRawStore) GetAttestationsByIDs(ids []string) ([]*types.As, error) {
	return GetAttestationsByIDs(s.db, ids)
}

// rfc3339 is a time as chrono's to_rfc3339 writes it from milliseconds, which
// is how ats-sqlite writes timestamp and created_at: UTC, +00:00, and three
// fractional digits only when there are milliseconds.
func rfc3339(t time.Time) string {
	t = t.UTC().Truncate(time.Millisecond)
	written := t.Format("2006-01-02T15:04:05")
	if ms := t.Nanosecond() / int(time.Millisecond); ms != 0 {
		written += fmt.Sprintf(".%03d", ms)
	}
	return written + "+00:00"
}

// jsonText is v as serde_json writes it: no HTML escaping and no trailing
// newline.
func jsonText(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
