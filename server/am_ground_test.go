package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
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

func TestAmGroundIsRootsAndSupers(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	assert.Equal(t, []string{"ROOT", "SUPER"}, compiled["/am/ground"])
}
