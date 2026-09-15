package storage

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// An attestation written through this node lands in the operational db
// first, then in S3 (ADR-037). S3 is the record, not what is read.

// "i want sentence 1 to be true"

// landsFirst is a raw store over two: the one a write lands in first, and
// the record it is written through to. Reads still go to the record: which
// store answers a read is sentence 2, and this is sentence 1.
type landsFirst struct {
	first  RawAttestationStore
	record RawAttestationStore
}

// LandsFirst pairs the store a write lands in first with the record behind it.
func LandsFirst(first, record RawAttestationStore) RawAttestationStore {
	return &landsFirst{first: first, record: record}
}

// CreateAttestation writes the first store, then the record. A record that
// would not take the write is said, not swallowed: the attestation is in the
// first store, and the error names which of the two has it.
func (l *landsFirst) CreateAttestation(as *types.As) error {
	if err := l.first.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "attestation %s did not land in the operational db", as.ID)
	}
	if err := l.record.CreateAttestation(as); err != nil {
		return errors.Wrapf(err, "attestation %s is in the operational db but its record was not written", as.ID)
	}
	return nil
}

func (l *landsFirst) GetAttestation(id string) (*types.As, error) {
	return l.record.GetAttestation(id)
}

func (l *landsFirst) AttestationExists(id string) bool {
	return l.record.AttestationExists(id)
}

func (l *landsFirst) CountAttestations() (int, error) {
	return l.record.CountAttestations()
}

// GetAttestations asks the record, which is the store that answers queries
// until sentence 2 lands.
func (l *landsFirst) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	q, ok := l.record.(QueryableStore)
	if !ok {
		return nil, errors.New("the record behind this store does not answer filter queries")
	}
	return q.GetAttestations(filter)
}

func (l *landsFirst) GetAttestationsByIDs(ids []string) ([]*types.As, error) {
	b, ok := l.record.(BatchGetStore)
	if !ok {
		return nil, errors.New("the record behind this store does not resolve ids in one statement")
	}
	return b.GetAttestationsByIDs(ids)
}
