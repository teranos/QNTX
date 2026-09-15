package storage

import (
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// An attestation written through this node lands in the operational db
// first, then in S3 (ADR-037). Every read is answered from the operational
// db. S3 is the record, not what is read.

// "i want sentence 1 to be true"
// "and i would like sentence 2 to be true as well"

// landsFirst is a raw store over two: the one a write lands in first and
// every read comes from, and the record it is written through to.
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
	return l.first.GetAttestation(id)
}

func (l *landsFirst) AttestationExists(id string) bool {
	return l.first.AttestationExists(id)
}

func (l *landsFirst) CountAttestations() (int, error) {
	return l.first.CountAttestations()
}

// GetAttestations asks the operational db, which is what answers a read.
func (l *landsFirst) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	q, ok := l.first.(QueryableStore)
	if !ok {
		return nil, errors.New("the operational db behind this store does not answer filter queries")
	}
	return q.GetAttestations(filter)
}

// GetAttestationsByIDs resolves ids in one statement when the operational db
// can, and one at a time when it cannot.
func (l *landsFirst) GetAttestationsByIDs(ids []string) ([]*types.As, error) {
	if b, ok := l.first.(BatchGetStore); ok {
		return b.GetAttestationsByIDs(ids)
	}
	out := make([]*types.As, 0, len(ids))
	for _, id := range ids {
		as, err := l.first.GetAttestation(id)
		if err != nil {
			return nil, err
		}
		if as != nil {
			out = append(out, as)
		}
	}
	return out, nil
}

// TakenIn is what opening a namespace found in its record: how many
// attestations the record held past the operational db's newest, and how many
// of those the db lacked and took in.
type TakenIn struct {
	Newest  time.Time
	Found   int
	TakenIn int
}

// TakeIn reads the record once, from the operational db's newest attestation
// on, and takes in what the db lacks. The newest attestation's own timestamp
// is read again, inclusive, because a second one can share it. An empty db is
// a first open, and takes in the whole record.
func TakeIn(first, record RawAttestationStore) (TakenIn, error) {
	var done TakenIn
	mine, ok := first.(QueryableStore)
	if !ok {
		return done, errors.New("the operational db does not answer filter queries, so nothing can be taken in")
	}
	theirs, ok := record.(QueryableStore)
	if !ok {
		return done, errors.New("the record does not answer filter queries, so nothing can be taken in")
	}

	newest, err := mine.GetAttestations(ats.AttestationFilter{Limit: 1})
	if err != nil {
		return done, errors.Wrap(err, "the operational db did not say its newest attestation")
	}
	filter := ats.AttestationFilter{}
	if len(newest) > 0 {
		done.Newest = newest[0].Timestamp
		since := newest[0].Timestamp
		filter.TimeStart = &since
	}

	held, err := theirs.GetAttestations(filter)
	if err != nil {
		return done, errors.Wrap(err, "the record did not answer, so nothing was taken in")
	}
	done.Found = len(held)

	// The record answers newest first; the db takes them oldest first, so
	// what it holds is a prefix of the record at every step.
	for i := len(held) - 1; i >= 0; i-- {
		as := held[i]
		if first.AttestationExists(as.ID) {
			continue
		}
		if err := first.CreateAttestation(as); err != nil {
			return done, errors.Wrapf(err, "attestation %s from the record did not land in the operational db", as.ID)
		}
		done.TakenIn++
	}
	return done, nil
}
