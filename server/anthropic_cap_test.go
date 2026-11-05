package server

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// 「[Opus 5.5] What ROOT gives is handed to Anthropic in whole cents, read as text: no float rounds it.」
func TestACapIsHandedToAnthropicInWholeCents(t *testing.T) {
	for given, cents := range map[string]string{"200": "20000", "12.5": "1250", "12.50": "1250", "0.01": "1", "007": "700", " 30 ": "3000"} {
		got, err := centsOf(given)
		require.NoError(t, err, given)
		assert.Equal(t, cents, got, given)
	}
	for _, given := range []string{"", "0", "0.00", "-5", "1.234", "12.", ".5", "ten", "1,5"} {
		_, err := centsOf(given)
		assert.Error(t, err, given)
	}
}

// "what  is max cap? like set it in the ui and make default of 200"
// 「[Opus 5.5] Set none, the cap is the node's default; ROOT's is kept on the ANTHROPIC line,」
// 「[Opus 5.5] and the order kept beside it is left as it was.」
func TestRootSetsTheCapOnASessionAtAnthropicAndTheDefaultIs200(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	before := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, "200", before.GetCap())
	assert.Equal(t, anthropicCapCurrency, before.GetCapCurrency())
	assert.True(t, before.GetCapDefault())

	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	set := rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "150"})
	assert.Equal(t, "150", set.GetCap())
	assert.False(t, set.GetCapDefault())
	assert.Equal(t, anthropicConsole, set.GetFirst(), "the order is kept with the cap")
	assert.True(t, set.GetThen())

	ordered := rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicSession})
	assert.Equal(t, "150", ordered.GetCap(), "the cap is kept with a new order")

	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://example.org/root"
	asked := httptest.NewRequest(http.MethodPost, "/api/anthropic/cap", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	for _, amount := range []string{"0", "ten", "1.234"} {
		_, refused := s.anthropicSignum().Answers["cap"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": amount})
		require.NotNil(t, refused, amount)
		assert.Equal(t, "amount", refused.GetParam())
	}
	assert.Equal(t, "150", rootAsksAnthropic(t, s, "am", sigil.Sent{}).GetCap(), "a refused cap changes nothing")
}

// Qodo's review of #1519: once ROOT had set a cap, nothing took the node back
// to its default. An amount left out does.
func TestRootTakesTheCapBackToTheDefault(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "150"})

	back := rootAsksAnthropic(t, s, "cap", sigil.Sent{})
	assert.Equal(t, anthropicDefaultCap, back.GetCap())
	assert.True(t, back.GetCapDefault())
	assert.Equal(t, anthropicConsole, back.GetFirst(), "the order is kept")
	assert.True(t, back.GetThen())
	again := rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "  "})
	assert.True(t, again.GetCapDefault(), "an amount of nothing but spaces is left out too")
}

// Qodo's review of QNTX-Web#55, deferred to the node: an order and a cap asked
// at once each read the ANTHROPIC line and wrote it anew, so the one written
// second carried the other's old value, and the first was lost.
func TestAnOrderAndACapAskedAtOnceBothStand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _, _ := runningNamespaceAgents(t)
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://example.org/root"
	asked := httptest.NewRequest(http.MethodPost, "/api/anthropic", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	ctx := sigil.WithCaller(asked.Context(), asked)
	signum := s.anthropicSignum()
	for round := 0; round < 40; round++ {
		first := []string{anthropicSession, anthropicConsole}[round%2]
		amount := strconv.Itoa(100 + round)
		var both sync.WaitGroup
		refusals := make(chan string, 2)
		both.Add(2)
		go func() {
			defer both.Done()
			if _, refused := signum.Answers["order"](ctx, sigil.Sent{"first": first, "then": "true"}); refused != nil {
				refusals <- refused.GetSays()
			}
		}()
		go func() {
			defer both.Done()
			if _, refused := signum.Answers["cap"](ctx, sigil.Sent{"amount": amount}); refused != nil {
				refusals <- refused.GetSays()
			}
		}()
		both.Wait()
		close(refusals)
		for says := range refusals {
			t.Fatalf("round %d: %s", round, says)
		}
		am := rootAsksAnthropic(t, s, "am", sigil.Sent{})
		require.Equal(t, amount, am.GetCap(), "round %d: the cap stands", round)
		require.Equal(t, first, am.GetFirst(), "round %d: the order stands", round)
	}
}

// Qodo's review of #1538: a store keeps a line's timestamp to the millisecond,
// and the newest line is the one with the latest timestamp, so a line written
// in the same millisecond as the one before it, or with the clock behind it,
// was not read as the newest and its change was written away by the next.
func TestAnOrderAndACapStandAfterALineStampedAheadOfTheClock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _, _ := runningNamespaceAgents(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	ahead := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-anthropic-ahead", Subjects: []string{anthropicLineSubject}, Predicates: []string{anthropicLinePredicate},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"https://example.org/root"}, Timestamp: ahead, CreatedAt: ahead,
		Source: "anthropic", Attributes: map[string]any{"first": anthropicSession, "then": false},
	}))

	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "75"})

	am := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, anthropicConsole, am.GetFirst(), "the order stands")
	assert.True(t, am.GetThen())
	assert.Equal(t, "75", am.GetCap(), "the cap stands")
}

// anthropicLinesHeld is every ANTHROPIC line the node keeps, as the store answers them.
func anthropicLinesHeld(t *testing.T, s *QNTXServer) []*types.As {
	t.Helper()
	reading, err := s.held.Read(auth.NamespaceSystem)
	require.NoError(t, err)
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{anthropicLineSubject}, Limit: ats.EveryRow})
	require.NoError(t, err)
	return found
}

// countedStore is a test store as storage.TakeIn asks of one: it counts what it holds.
type countedStore struct{ ats.AttestationStore }

func (c countedStore) CountAttestations() (int, error) {
	all, err := c.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow})
	return len(all), err
}

func (c countedStore) GetAttestation(id string) (*types.As, error) {
	all, err := c.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow})
	for _, as := range all {
		if as.ID == id {
			return as, err
		}
	}
	return nil, err
}

// heldMark is a take-in mark held in memory.
type heldMark struct {
	at     time.Time
	marked bool
}

func (m *heldMark) Read() (time.Time, bool, error) { return m.at, m.marked, nil }
func (m *heldMark) Write(at time.Time) error       { m.at, m.marked = at, true; return nil }

// Qodo's review of #1538: a line stamped ahead of the clock became the take-in
// mark (storage.TakeIn reads the record from its newest timestamp on), so a row
// stamped by the clock that reached the record after was never taken in. A
// line's timestamp is when it was written; its place among the node's lines is
// the revision it carries.
func TestAnAnthropicLineIsStampedWhenItIsWrittenAndOrderedByItsRevision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _, _ := runningNamespaceAgents(t)

	// Two changes in one millisecond both stand.
	instant := time.Now().Truncate(time.Millisecond)
	anthropicNow = func() time.Time { return instant }
	t.Cleanup(func() { anthropicNow = time.Now })
	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "75"})
	am := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, anthropicConsole, am.GetFirst(), "the order stands")
	assert.Equal(t, "75", am.GetCap(), "the cap stands")
	for _, line := range anthropicLinesHeld(t, s) {
		assert.Equal(t, instant.UnixMilli(), line.Timestamp.UnixMilli(), "a line is stamped when it was written, not after the one before")
	}

	// A line already stamped ahead of the clock does not make the next one stamp ahead.
	anthropicNow = time.Now
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	ahead := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-anthropic-stamped-ahead", Subjects: []string{anthropicLineSubject}, Predicates: []string{anthropicLinePredicate},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"https://example.org/root"}, Timestamp: ahead, CreatedAt: ahead,
		Source: "anthropic", Attributes: map[string]any{"first": anthropicConsole, "then": true, "cap": "75", "revision": 2},
	}))
	before := time.Now()
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "80"})
	after := time.Now()
	assert.Equal(t, "80", rootAsksAnthropic(t, s, "am", sigil.Sent{}).GetCap())
	var newest *types.As
	for _, line := range anthropicLinesHeld(t, s) {
		if line.ID != "AS-anthropic-stamped-ahead" && line.Timestamp.After(instant.Add(time.Millisecond)) {
			newest = line
		}
	}
	require.NotNil(t, newest)
	assert.False(t, newest.Timestamp.Before(before.Truncate(time.Millisecond)) || newest.Timestamp.After(after), "stamped %s, written between %s and %s", newest.Timestamp, before, after)

	// Qodo's case: the record took in a revision, then a row stamped by the clock reached it; the next take-in takes that row in.
	record, _ := createTestStore(t)
	first, _ := createTestStore(t)
	require.NoError(t, record.CreateAttestation(newest))
	mark := &heldMark{}
	_, err = storage.TakeIn(countedStore{first}, countedStore{record}, mark)
	require.NoError(t, err)
	later := time.Now()
	require.NoError(t, record.CreateAttestation(&types.As{
		ID: "AS-written-after", Subjects: []string{"SOMETHING"}, Predicates: []string{"happened"}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{"https://example.org/root"}, Timestamp: later, CreatedAt: later, Source: "test",
	}))
	_, err = storage.TakeIn(countedStore{first}, countedStore{record}, mark)
	require.NoError(t, err)
	assert.True(t, first.AttestationExists("AS-written-after"), "a row written after the revision is taken in")
}

// As Qodo found for name lines on #1540: a build from before revisions, run
// after one that writes them (a node flipped to an older branch), writes a
// line with no revision. Read as older than every line with one, the change
// it made was written away. It is the newest when it was written after the
// newest line with a revision.
func TestAnAnthropicLineWithNoRevisionAfterARevisionedOneIsTheNewest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _, _ := runningNamespaceAgents(t)
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "75"})
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	later := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-anthropic-by-an-older-build", Subjects: []string{anthropicLineSubject}, Predicates: []string{anthropicLinePredicate},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"https://example.org/root"}, Timestamp: later, CreatedAt: later,
		Source: "anthropic", Attributes: map[string]any{"first": anthropicConsole, "then": true, "cap": "90"},
	}))
	am := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, "90", am.GetCap())
	assert.Equal(t, anthropicConsole, am.GetFirst())

	// The next change comes after both.
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "95"})
	am = rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, "95", am.GetCap())
	assert.Equal(t, anthropicConsole, am.GetFirst(), "carried from the line the older build wrote")
}
