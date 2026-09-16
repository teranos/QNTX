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

// Mark is where the operational db remembers how far into the record its last
// take-in reached. It is a fact about the take-in and not about the rows: a
// file that started taking writes before it ever read the record holds rows
// newer than everything it lacks.
type Mark interface {
	// Read is the watermark, and false when no take-in has finished.
	Read() (time.Time, bool, error)
	Write(at time.Time) error
}

// TakenIn is what opening a namespace found in its record: from when it
// read, how many attestations the record answered, and how many of those the
// operational db lacked and took in.
type TakenIn struct {
	Since   time.Time
	Whole   bool
	Found   int
	TakenIn int
}

// TakeIn reads the record once, from the mark on, and takes in what the
// operational db lacks. The mark's own instant is read again, inclusive,
// because a second attestation can share it. No mark, or an operational db
// holding nothing, is a first open and takes in the whole record; the mark is
// written only after every row landed, so a take-in cut short is done again.
func TakeIn(first, record RawAttestationStore, mark Mark) (TakenIn, error) {
	var done TakenIn
	theirs, ok := record.(QueryableStore)
	if !ok {
		return done, errors.New("the record does not answer filter queries, so nothing can be taken in")
	}

	since, marked, err := mark.Read()
	if err != nil {
		return done, errors.Wrap(err, "the take-in mark could not be read")
	}
	held, err := first.CountAttestations()
	if err != nil {
		return done, errors.Wrap(err, "the operational db did not say how many attestations it holds")
	}
	filter := ats.AttestationFilter{}
	if marked && held > 0 {
		done.Since = since
		filter.TimeStart = &since
	} else {
		done.Whole = true
	}

	answered, err := theirs.GetAttestations(filter)
	if err != nil {
		return done, errors.Wrap(err, "the record did not answer, so nothing was taken in")
	}
	done.Found = len(answered)

	// The record answers newest first; the db takes them oldest first.
	for i := len(answered) - 1; i >= 0; i-- {
		as := answered[i]
		if first.AttestationExists(as.ID) {
			continue
		}
		if err := first.CreateAttestation(as); err != nil {
			return done, errors.Wrapf(err, "attestation %s from the record did not land in the operational db", as.ID)
		}
		done.TakenIn++
	}

	// The record's newest is how far this take-in reached. A record that
	// answered nothing leaves the mark where it was, or unwritten.
	if len(answered) > 0 {
		if err := mark.Write(answered[0].Timestamp); err != nil {
			return done, errors.Wrap(err, "the take-in mark could not be written")
		}
	}
	return done, nil
}
