package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap/zaptest"
)

const alice = "https://mastodon.example/@alice"

func left(l *newsLog, n News) {
	n.UntilMs = time.Now().Add(time.Minute).UnixMilli()
	l.leave(n)
}

// "this part is supposed to show things QNTX does for ground specifically"
func TestAmGroundSaysWhatTheNodeDoesForGround(t *testing.T) {
	l := newNewsLog()
	left(l, News{ID: "p-1:162d82f", For: alice, Item: StatusItem{Name: "ci", Note: "success main 162d82f", Symbol: SymbolWell},
		Detail: map[string]any{"repo": "teranos/ground", "conclusion": "success"}})
	left(l, News{ID: "p-2:watching", For: alice, Item: StatusItem{Name: "ci", Note: "watching main 9f3c1aa 2 runs", Symbol: SymbolWell}, Quiet: true})
	left(l, News{ID: "p-9", For: "https://mastodon.example/@bob", Item: StatusItem{Name: "ci", Note: "failure main", Symbol: SymbolUnwell}})
	failures := newHandlerFailureLog()
	failures.record(HandlerFailure{Handler: watcher.CIWatchHandlerName, ExecutionID: "rearm:p-0", Error: "ci.watch: stopped waiting on main@162d82f: context canceled", AtMs: time.Now().UnixMilli()})
	failures.record(HandlerFailure{Handler: "garden/harvest", Error: "no water", AtMs: time.Now().UnixMilli()})
	s := &QNTXServer{news: l, handlerFailures: failures, startedAt: time.Date(2026, 10, 3, 21, 58, 0, 0, time.UTC)}
	signum := s.amSignum()
	require.NoError(t, signum.Check())

	answer, refused := signum.Answers["ground"](auth.WithAdmission(context.Background(), tokenCaller("did:key:alice")), sigil.Sent{})
	require.Nil(t, refused)
	holds(t, signum, "ground", answer)
	said := answer.(map[string]any)

	assert.Equal(t, "2026-10-03T21:58:00Z", said["started"])
	assert.Equal(t, int64(1), said["left"], "the conclusion is counted, the wait is not, and bob's is bob's")

	news := said["news"].([]map[string]any)
	require.Len(t, news, 2)
	byID := map[string]map[string]any{}
	for _, n := range news {
		byID[n["id"].(string)] = n
	}
	assert.Equal(t, true, byID["p-2:watching"]["waiting"])
	assert.Equal(t, false, byID["p-1:162d82f"]["waiting"])
	assert.Equal(t, "success main 162d82f", byID["p-1:162d82f"]["note"])
	assert.Equal(t, "teranos/ground", byID["p-1:162d82f"]["repo"], "a conclusion carries what a click on it answers")
	assert.Equal(t, true, byID["p-1:162d82f"]["on_row"])

	var watched []string
	for _, w := range said["watches"].([]map[string]any) {
		watched = append(watched, w["id"].(string))
	}
	assert.Equal(t, []string{watcher.StandingCIPushed, watcher.StandingDispatchSent}, watched)

	failed := said["failed"].([]map[string]any)
	require.Len(t, failed, 1, "another handler's failure is not ci.watch's")
	assert.Equal(t, "rearm:p-0", failed[0]["execution_id"])
}

// Spike: a wait is left again on every turn of its loop, and is never a conclusion.
func TestAWaitIsNeverCountedAsLeft(t *testing.T) {
	l := newNewsLog()
	for range 3 {
		left(l, News{ID: "p-2:watching", For: alice, Item: StatusItem{Name: "ci", Symbol: SymbolWell}, Quiet: true})
	}
	held, count := l.leftFor(alice)
	require.Len(t, held, 1)
	assert.Equal(t, int64(0), count)

	left(l, News{ID: "p-2:9f3c1aa", For: alice, Item: StatusItem{Name: "ci", Symbol: SymbolWell}})
	_, count = l.leftFor(alice)
	assert.Equal(t, int64(1), count)
}

// Spike: what has left the row is still what the node did, until the process ends.
func TestWhatLeftTheRowIsStillSaid(t *testing.T) {
	l := newNewsLog()
	l.leave(News{ID: "p-0:old", For: alice, Item: StatusItem{Name: "ci", Note: "success main", Symbol: SymbolWell}, UntilMs: time.Now().Add(-time.Hour).UnixMilli()})
	s := &QNTXServer{news: l}

	answer, refused := s.amGround(auth.WithAdmission(context.Background(), tokenCaller("did:key:alice")), sigil.Sent{})
	require.Nil(t, refused)
	news := answer.(map[string]any)["news"].([]map[string]any)
	require.Len(t, news, 1)
	assert.Equal(t, false, news[0]["on_row"])
}

// Spike: a node that knows nobody has left nothing for anybody, and still says what it watches.
func TestAmGroundForNobody(t *testing.T) {
	l := newNewsLog()
	left(l, News{ID: "p-1", For: alice, Item: StatusItem{Name: "ci", Symbol: SymbolWell}})
	s := &QNTXServer{news: l}

	answer, refused := s.amGround(context.Background(), sigil.Sent{})
	require.Nil(t, refused)
	said := answer.(map[string]any)
	assert.Empty(t, said["news"])
	assert.Equal(t, int64(0), said["left"])
	assert.Equal(t, "", said["started"])
	assert.Len(t, said["watches"], 2)
}

// One usage reading as ug's status line posts it (ground ug/usage.d): the
// window is the subject, the session the context, and ug is among the actors.
func reading(id, window, session string, at time.Time, used float64) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{window},
		Predicates: []string{ugReadingPredicate},
		Contexts:   []string{"session:" + session},
		Actors:     []string{"did:key:alice", ugActor},
		Timestamp:  at,
		CreatedAt:  at,
		Attributes: map[string]any{"used_percentage": used, "resets_at": float64(1791079800)},
	}
}

// "this seems like a QNTX change from the backend side, have we tackled this yet?"
func TestAmGroundSaysWhatUgPosted(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	now := time.Now().UTC().Truncate(time.Hour)
	rows := []*types.As{
		reading("r-1", "five_hour", "s-1", now.Add(-3*time.Hour), 4),
		reading("r-2", "five_hour", "s-2", now.Add(-2*time.Hour), 9),
		reading("r-3", "five_hour", "s-1", now.Add(-time.Hour), 13),
		reading("r-4", "seven_day", "s-1", now.Add(-time.Hour), 61),
		reading("r-old", "five_hour", "s-0", now.Add(-48*time.Hour), 80),
		streamed("ground:payload:Stop:1", "Stop", "s-1", now.Add(-time.Hour), map[string]any{"last_assistant_message": "done"}),
	}
	for _, as := range rows {
		require.NoError(t, store.CreateAttestation(as))
	}
	s := &QNTXServer{held: servingOne(db, store), logger: zaptest.NewLogger(t).Sugar()}
	asked := httptest.NewRequest(http.MethodGet, "/am/ground", nil)

	answer, refused := s.amGround(sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.Nil(t, refused)
	holds(t, s.amSignum(), "ground", answer)
	ug := answer.(map[string]any)["ug"].(map[string]any)

	sessions := ug["sessions"].([]map[string]any)
	require.Len(t, sessions, 2, "a reading two days old is outside the day, and a Stop is no reading")
	assert.Equal(t, "s-1", sessions[0]["session"], "the session that posted last comes first")
	assert.Equal(t, 3, sessions[0]["readings"])
	assert.Equal(t, map[string]int{
		now.Add(-3 * time.Hour).Format(hourBucket): 1,
		now.Add(-time.Hour).Format(hourBucket):     2,
	}, sessions[0]["over"])
	assert.Equal(t, "s-2", sessions[1]["session"])

	windows := ug["windows"].([]map[string]any)
	require.Len(t, windows, 2)
	assert.Equal(t, "five_hour", windows[0]["window"])
	five := windows[0]["readings"].([]map[string]any)
	require.Len(t, five, 3)
	assert.Equal(t, 4.0, five[0]["used"], "a window's readings come oldest first")
	assert.Equal(t, 13.0, five[2]["used"])
}

// Tim: the tmux bar asks in its own format, and that is what the node remembers.
func TestTheRowRemembersWhenTmuxAsked(t *testing.T) {
	h := newsRow(newNewsLog())
	for _, format := range []string{FormatTmux, FormatTmux, FormatJSON} {
		req := httptest.NewRequest(http.MethodGet, "/am/statusline?format="+format, nil)
		req = req.WithContext(auth.WithAdmission(req.Context(), tokenCaller("did:key:alice")))
		h.HandleStatusLine(httptest.NewRecorder(), req)
	}

	_, count, over, asked := h.tmux.of(alice)
	require.True(t, asked)
	assert.Equal(t, int64(2), count, "an ask in json is a browser's, not a bar's")
	var byTheMinute int64
	for _, n := range over {
		byTheMinute += n
	}
	assert.Equal(t, int64(2), byTheMinute)

	_, _, _, bobAsked := h.tmux.of("https://mastodon.example/@bob")
	assert.False(t, bobAsked)
}

// Spike: the count is of the whole process, and the minutes are of the last hour.
func TestAsksOlderThanAnHourAreLetGo(t *testing.T) {
	a := newRowAsks()
	at := time.Date(2026, 10, 3, 22, 0, 0, 0, time.UTC)
	a.note(alice, at)
	a.note(alice, at.Add(90*time.Minute))

	lastMs, count, over, asked := a.of(alice)
	require.True(t, asked)
	assert.Equal(t, at.Add(90*time.Minute).UnixMilli(), lastMs)
	assert.Equal(t, int64(2), count)
	assert.Equal(t, map[string]int64{"2026-10-03T23:30": 1}, over)
}

func TestAmGroundIsRootsAndSupers(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	assert.Equal(t, []string{"ROOT", "SUPER"}, compiled["/am/ground"])
}
