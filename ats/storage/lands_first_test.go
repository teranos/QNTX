package storage

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/errors"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// rawInOrder is a raw store that writes into a shared order, so a test can
// say which of two stores took a write first. It answers queries the way both
// backends do: newest first, from a time on, up to a limit.
type rawInOrder struct {
	name    string
	order   *[]string
	held    map[string]*types.As
	refuses error
	queries int
}

func newRawInOrder(name string, order *[]string) *rawInOrder {
	return &rawInOrder{name: name, order: order, held: map[string]*types.As{}}
}

func (r *rawInOrder) CreateAttestation(as *types.As) error {
	if r.refuses != nil {
		return r.refuses
	}
	*r.order = append(*r.order, r.name+":"+as.ID)
	r.held[as.ID] = as
	return nil
}

func (r *rawInOrder) GetAttestation(id string) (*types.As, error) { return r.held[id], nil }
func (r *rawInOrder) AttestationExists(id string) bool            { _, ok := r.held[id]; return ok }
func (r *rawInOrder) CountAttestations() (int, error)             { return len(r.held), nil }
func (r *rawInOrder) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	r.queries++
	out := make([]*types.As, 0, len(r.held))
	for _, as := range r.held {
		if filter.TimeStart != nil && as.Timestamp.Before(*filter.TimeStart) {
			continue
		}
		out = append(out, as)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func at(id string, second int) *types.As {
	return &types.As{ID: id, Timestamp: time.Date(2026, 9, 15, 0, 0, second, 0, time.UTC)}
}

func TestAWriteLandsInTheFirstStoreThenTheRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	pair := LandsFirst(first, record)

	require.NoError(t, pair.CreateAttestation(at("AS-1", 1)))
	assert.Equal(t, []string{"operational db:AS-1", "S3:AS-1"}, order)
	assert.True(t, first.AttestationExists("AS-1"))
	assert.True(t, record.AttestationExists("AS-1"))
}

func TestARecordThatWillNotTakeTheWriteIsSaidAndTheFirstStoreKeepsIt(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	record.refuses = errors.New("S3 said no")
	pair := LandsFirst(first, record)

	err := pair.CreateAttestation(at("AS-1", 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AS-1 is in the operational db but its record was not written")
	assert.Contains(t, err.Error(), "S3 said no")
	assert.True(t, first.AttestationExists("AS-1"))
	assert.False(t, record.AttestationExists("AS-1"))
}

func TestAFirstStoreThatWillNotTakeTheWriteWritesNoRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	first.refuses = errors.New("disk full")
	record := newRawInOrder("S3", &order)
	pair := LandsFirst(first, record)

	err := pair.CreateAttestation(at("AS-1", 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AS-1 did not land in the operational db")
	assert.Empty(t, order)
	assert.False(t, record.AttestationExists("AS-1"))
}

func TestReadsAreAnsweredByTheOperationalDbAndNeverTheRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	first.held["AS-mine"] = at("AS-mine", 1)
	record := newRawInOrder("S3", &order)
	record.held["AS-theirs"] = at("AS-theirs", 2)
	pair := LandsFirst(first, record)

	assert.True(t, pair.AttestationExists("AS-mine"))
	assert.False(t, pair.AttestationExists("AS-theirs"), "a read reached the record")
	got, err := pair.GetAttestation("AS-mine")
	require.NoError(t, err)
	assert.Equal(t, "AS-mine", got.ID)
	n, err := pair.CountAttestations()
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	held, err := pair.(QueryableStore).GetAttestations(ats.AttestationFilter{})
	require.NoError(t, err)
	assert.Len(t, held, 1)
	many, err := pair.(BatchGetStore).GetAttestationsByIDs([]string{"AS-mine", "AS-theirs"})
	require.NoError(t, err)
	assert.Len(t, many, 1)
	assert.Equal(t, 0, record.queries, "a read reached the record")
}

// memMark is a Mark held in memory, so a test can say what was remembered.
type memMark struct {
	at     time.Time
	marked bool
	writes int
}

func (m *memMark) Read() (time.Time, bool, error) { return m.at, m.marked, nil }
func (m *memMark) Write(at time.Time) error       { m.at, m.marked = at, true; m.writes++; return nil }

func TestAFirstOpenTakesInTheWholeRecordOldestFirstAndRemembersHowFar(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	for i, id := range []string{"AS-a", "AS-b", "AS-c"} {
		record.held[id] = at(id, i+1)
	}
	mark := &memMark{}

	done, err := TakeIn(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, TakenIn{Whole: true, Found: 3, TakenIn: 3}, done)
	assert.Equal(t, []string{"operational db:AS-a", "operational db:AS-b", "operational db:AS-c"}, order)
	assert.True(t, mark.marked)
	assert.Equal(t, at("AS-c", 3).Timestamp, mark.at, "the mark is the record's newest")
}

func TestALaterOpenTakesInOnlyWhatIsPastTheMark(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	for i, id := range []string{"AS-a", "AS-b"} {
		first.held[id] = at(id, i+1)
		record.held[id] = at(id, i+1)
	}
	// One that shares the marked second, and one past it.
	record.held["AS-b2"] = at("AS-b2", 2)
	record.held["AS-c"] = at("AS-c", 3)
	mark := &memMark{at: at("AS-b", 2).Timestamp, marked: true}

	done, err := TakeIn(first, record, mark)
	require.NoError(t, err)
	assert.False(t, done.Whole)
	assert.Equal(t, at("AS-b", 2).Timestamp, done.Since)
	assert.Equal(t, 3, done.Found, "AS-b, AS-b2 and AS-c are at or past the mark")
	assert.Equal(t, 2, done.TakenIn)
	assert.True(t, first.AttestationExists("AS-b2"))
	assert.True(t, first.AttestationExists("AS-c"))
	assert.Equal(t, 1, record.queries, "the record was read more than once")
	assert.Equal(t, at("AS-c", 3).Timestamp, mark.at)
}

// The landing files on the box took writes for a quarter hour before they ever
// read the record. Their newest row said nothing about what they lacked.
func TestAFileThatTookWritesBeforeEverReadingTheRecordTakesInTheWhole(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	record.held["AS-old"] = at("AS-old", 1)
	first.held["AS-new"] = at("AS-new", 9)
	record.held["AS-new"] = at("AS-new", 9)

	done, err := TakeIn(first, record, &memMark{})
	require.NoError(t, err)
	assert.True(t, done.Whole)
	assert.Equal(t, 2, done.Found)
	assert.Equal(t, 1, done.TakenIn)
	assert.True(t, first.AttestationExists("AS-old"))
}

func TestAMarkOnAnEmptyFileIsNotBelieved(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	record.held["AS-old"] = at("AS-old", 1)
	mark := &memMark{at: at("AS-old", 5).Timestamp, marked: true}

	done, err := TakeIn(first, record, mark)
	require.NoError(t, err)
	assert.True(t, done.Whole, "a file holding nothing was trusted to be behind its mark")
	assert.Equal(t, 1, done.TakenIn)
}

func TestARecordThatDoesNotAnswerTakesNothingInAndMarksNothing(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	mark := &memMark{}
	_, err := TakeIn(first, brokenRaw{}, mark)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the record did not answer")
	assert.False(t, mark.marked)
}

func TestATakeInCutShortMarksNothing(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	first.refuses = errors.New("disk full")
	record := newRawInOrder("S3", &order)
	record.held["AS-a"] = at("AS-a", 1)
	mark := &memMark{}

	_, err := TakeIn(first, record, mark)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AS-a from the record did not land in the operational db")
	assert.False(t, mark.marked, "a mark was written for a take-in that did not finish")
}

func TestAFileMarkRoundTripsAndIsAbsentUntilWritten(t *testing.T) {
	mark := FileMark{Path: t.TempDir() + "/pond.db.taken-in"}
	_, marked, err := mark.Read()
	require.NoError(t, err)
	assert.False(t, marked)

	require.NoError(t, mark.Write(at("AS-a", 7).Timestamp))
	got, marked, err := mark.Read()
	require.NoError(t, err)
	assert.True(t, marked)
	assert.True(t, got.Equal(at("AS-a", 7).Timestamp))
}

type brokenRaw struct{}

func (brokenRaw) CreateAttestation(*types.As) error        { return errors.New("no") }
func (brokenRaw) GetAttestation(string) (*types.As, error) { return nil, errors.New("no") }
func (brokenRaw) AttestationExists(string) bool            { return false }
func (brokenRaw) CountAttestations() (int, error)          { return 0, errors.New("no") }
func (brokenRaw) GetAttestations(ats.AttestationFilter) ([]*types.As, error) {
	return nil, errors.New("S3 is not answering")
}
