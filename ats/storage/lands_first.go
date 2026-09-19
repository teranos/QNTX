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

// The landing file is the buffer. A write lands there and nowhere else; what
// it holds past the send mark reaches the record when it is sent out. A
// process that dies keeps every row it accepted, and the next send carries
// them. Only losing the host loses what was not yet sent.

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

// Landed is a landing file read in the order its rows landed.
type Landed interface {
	RawAttestationStore
	QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error)
}

// FileWriter is the record taking a batch of attestations as one file.
type FileWriter interface {
	WriteFile(attestations []*types.As) (int, error)
}

// SentMark is the id of the last attestation the landing file sent to the
// record. An id and not an instant: rows land in an order their timestamps do
// not keep, and the landing order is what a send walks.
type SentMark interface {
	// Read is the last id sent, and false when nothing was ever sent.
	Read() (string, bool, error)
	Write(id string) error
}

// sendBatch is how many rows one file takes. Six hours of a namespace taking
// a gigabyte a month is 1224 rows, so a send is one file until an outage
// leaves more behind.
const sendBatch = 5000

// pastTheMark is every column the landing file holds, in landing order, from
// the row after the one the mark names.
const pastTheMark = `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
FROM attestations
WHERE rowid > (SELECT rowid FROM attestations WHERE id = ?)
ORDER BY rowid
LIMIT ?`

// fromTheStart is pastTheMark for a file that has never sent.
const fromTheStart = `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
FROM attestations
ORDER BY rowid
LIMIT ?`

// lastLanded is the newest row the landing file holds.
const lastLanded = `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
FROM attestations
ORDER BY rowid DESC
LIMIT 1`

// SendOut writes what the landing file holds past the mark to the record, a
// batch to a file, moving the mark after each file. A send cut short leaves
// the mark at the last file that was written, so the next send starts there.
//
// A mark naming an id the landing file does not hold is an error rather than
// a send from the start: that would write the whole namespace to the record a
// second time.
func SendOut(first Landed, record FileWriter, mark SentMark) (int, error) {
	last, marked, err := mark.Read()
	if err != nil {
		return 0, errors.Wrap(err, "the send mark could not be read")
	}
	if marked && !first.AttestationExists(last) {
		return 0, errors.Newf("the send mark names %s, which the landing file does not hold", last)
	}

	sent := 0
	for {
		var batch []*types.As
		if marked {
			batch, err = first.QueryAttestationsRaw(pastTheMark, []interface{}{last, sendBatch})
		} else {
			batch, err = first.QueryAttestationsRaw(fromTheStart, []interface{}{sendBatch})
		}
		if err != nil {
			return sent, errors.Wrap(err, "the landing file did not say what it holds past the send mark")
		}
		if len(batch) == 0 {
			return sent, nil
		}
		wrote, err := record.WriteFile(batch)
		if err != nil {
			return sent, errors.Wrapf(err, "%d attestations from the landing file were not written to the record", len(batch))
		}
		last, marked = batch[len(batch)-1].ID, true
		if err := mark.Write(last); err != nil {
			return sent, errors.Wrapf(err, "%d attestations reached the record and the send mark did not move past them", wrote)
		}
		sent += wrote
	}
}

// MarkAllSent moves the send mark to the newest row the landing file holds,
// for rows the record already has: a take-in's, or a file that never sent
// and whose rows were written to the record another way.
func MarkAllSent(first Landed, mark SentMark) error {
	newest, err := first.QueryAttestationsRaw(lastLanded, nil)
	if err != nil {
		return errors.Wrap(err, "the landing file did not say which row it took last")
	}
	if len(newest) == 0 {
		return nil
	}
	if err := mark.Write(newest[0].ID); err != nil {
		return errors.Wrap(err, "the send mark could not be written")
	}
	return nil
}
