package storage

import (
	"fmt"
	"os"
	"slices"
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

// landedInOrder is a landing file that keeps the order its rows landed in,
// and answers the three statements a send asks the way SQLite does.
type landedInOrder struct {
	*rawInOrder
	landed []string
}

func newLanded(ids ...string) *landedInOrder {
	var order []string
	l := &landedInOrder{rawInOrder: newRawInOrder("landing file", &order)}
	for i, id := range ids {
		l.land(at(id, i))
	}
	return l
}

func (l *landedInOrder) land(as *types.As) {
	l.held[as.ID] = as
	l.landed = append(l.landed, as.ID)
}

func (l *landedInOrder) QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error) {
	from, limit := 0, 0
	switch sql {
	case pastTheMark:
		from = slices.Index(l.landed, params[0].(string)) + 1
		limit = params[1].(int)
	case fromTheStart:
		limit = params[0].(int)
	case lastLanded:
		if len(l.landed) == 0 {
			return nil, nil
		}
		return []*types.As{l.held[l.landed[len(l.landed)-1]]}, nil
	default:
		return nil, errors.Newf("a statement a send does not ask: %s", sql)
	}
	var out []*types.As
	for _, id := range l.landed[from:] {
		if len(out) == limit {
			break
		}
		out = append(out, l.held[id])
	}
	return out, nil
}

// filesWritten is the record, as the files it was handed.
type filesWritten struct {
	files   [][]string
	refuses error
}

func (f *filesWritten) WriteFile(batch []*types.As) (int, error) {
	if f.refuses != nil {
		return 0, f.refuses
	}
	ids := make([]string, 0, len(batch))
	for _, as := range batch {
		ids = append(ids, as.ID)
	}
	f.files = append(f.files, ids)
	return len(batch), nil
}

// memSentMark is a SentMark held in memory.
type memSentMark struct {
	id     string
	marked bool
}

func (m *memSentMark) Read() (string, bool, error) { return m.id, m.marked, nil }
func (m *memSentMark) Write(id string) error       { m.id, m.marked = id, true; return nil }

func TestASendCarriesWhatLandedPastTheMarkInLandingOrder(t *testing.T) {
	// AS-late landed last with the oldest timestamp; landing order is what counts.
	first := newLanded("AS-a", "AS-b")
	first.land(at("AS-c", 9))
	first.land(at("AS-late", 0))
	record := &filesWritten{}
	mark := &memSentMark{id: "AS-a", marked: true}

	sent, err := SendOut(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, 3, sent)
	assert.Equal(t, [][]string{{"AS-b", "AS-c", "AS-late"}}, record.files)
	assert.Equal(t, "AS-late", mark.id)
}

func TestAFileThatNeverSentSendsFromItsFirstRow(t *testing.T) {
	first := newLanded("AS-a", "AS-b")
	record := &filesWritten{}
	mark := &memSentMark{}

	sent, err := SendOut(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, 2, sent)
	assert.Equal(t, [][]string{{"AS-a", "AS-b"}}, record.files)
	assert.Equal(t, "AS-b", mark.id)
}

func TestNothingPastTheMarkWritesNoFile(t *testing.T) {
	first := newLanded("AS-a")
	record := &filesWritten{}
	mark := &memSentMark{id: "AS-a", marked: true}

	sent, err := SendOut(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, 0, sent)
	assert.Empty(t, record.files)
}

// The rows stay past the mark, so the next send carries them.
func TestARecordThatWillNotTakeTheFileLeavesTheMarkWhereItWas(t *testing.T) {
	first := newLanded("AS-a", "AS-b")
	record := &filesWritten{refuses: errors.New("S3 said no")}
	mark := &memSentMark{id: "AS-a", marked: true}

	_, err := SendOut(first, record, mark)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3 said no")
	assert.Equal(t, "AS-a", mark.id)

	record.refuses = nil
	sent, err := SendOut(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, 1, sent)
	assert.Equal(t, [][]string{{"AS-b"}}, record.files)
}

// Sending from the start would write the whole namespace to the record again.
func TestAMarkNamingARowTheFileDoesNotHoldIsRefused(t *testing.T) {
	first := newLanded("AS-a")
	record := &filesWritten{}
	mark := &memSentMark{id: "AS-gone", marked: true}

	_, err := SendOut(first, record, mark)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the send mark names AS-gone, which the landing file does not hold")
	assert.Empty(t, record.files)
}

func TestMoreThanABatchIsSentAsSeveralFilesMarkingEach(t *testing.T) {
	first := newLanded()
	for i := 0; i <= sendBatch; i++ {
		first.land(at(fmt.Sprintf("AS-%05d", i), 0))
	}
	record := &filesWritten{}
	mark := &memSentMark{}

	sent, err := SendOut(first, record, mark)
	require.NoError(t, err)
	assert.Equal(t, sendBatch+1, sent)
	require.Len(t, record.files, 2)
	assert.Len(t, record.files[0], sendBatch)
	assert.Equal(t, []string{fmt.Sprintf("AS-%05d", sendBatch)}, record.files[1])
	assert.Equal(t, fmt.Sprintf("AS-%05d", sendBatch), mark.id)
}

func TestMarkAllSentMarksTheNewestLandedRow(t *testing.T) {
	first := newLanded("AS-a", "AS-b")
	mark := &memSentMark{}
	require.NoError(t, MarkAllSent(first, mark))
	assert.Equal(t, "AS-b", mark.id)
}

func TestMarkAllSentOnAnEmptyFileMarksNothing(t *testing.T) {
	mark := &memSentMark{}
	require.NoError(t, MarkAllSent(newLanded(), mark))
	assert.False(t, mark.marked)
}

func TestAFileSentMarkRoundTripsAndIsAbsentUntilWritten(t *testing.T) {
	mark := FileSentMark{Path: t.TempDir() + "/pond.db.sent"}
	_, marked, err := mark.Read()
	require.NoError(t, err)
	assert.False(t, marked)

	require.NoError(t, mark.Write("AS-a"))
	require.NoError(t, mark.Write("AS-b"))
	got, marked, err := mark.Read()
	require.NoError(t, err)
	assert.True(t, marked)
	assert.Equal(t, "AS-b", got)
}

func TestAnEmptySendMarkFileIsAnError(t *testing.T) {
	path := t.TempDir() + "/pond.db.sent"
	require.NoError(t, os.WriteFile(path, []byte("\n"), 0o640))
	_, _, err := FileSentMark{Path: path}.Read()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no attestation")
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
