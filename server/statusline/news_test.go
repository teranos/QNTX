package statusline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
)

const alice = "https://mastodon.example/@alice"

func newsRow(l *NewsLog) *StatusLineHandler {
	h := NewStatusLineHandler(nil, nil, nil,
		func() storage.Watchers { return nil },
		func() *HandlerFailureLog { return NewHandlerFailureLog() }, nil)
	h.News = func() *NewsLog { return l }
	return h
}

func rowAs(t *testing.T, h *StatusLineHandler, caller auth.Admission) StatusLineResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/am/statusline?format=json", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), caller))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v — %s", err, rec.Body.String())
	}
	return body
}

// A token of a person: it acts as its own DID and speaks for the identity
// that minted it, the way auth.go admits one.
func tokenCaller(did string) auth.Admission {
	a := auth.Admitted(auth.LevelSuper)
	a.Grant = &auth.Grant{DID: did, MintedBy: "https://mastodon.example/@" + did[len("did:key:"):]}
	a.Identity = a.Grant.MintedBy
	return a
}

// News is addressed to the person. Two people polling the same node each see
// what was left for them and nothing left for the other — and a person's
// second token, or their session, sees what their first token's push earned.
func TestNewsReachesOnlyWhoItIsFor(t *testing.T) {
	l := NewNewsLog()
	now := time.Now()
	l.Leave(News{ID: "n-alice", For: "https://mastodon.example/@alice", Item: StatusItem{Name: "ci", Note: "success main", Symbol: SymbolWell}, UntilMs: now.Add(time.Minute).UnixMilli()})
	l.Leave(News{ID: "n-bob", For: "https://mastodon.example/@bob", Item: StatusItem{Name: "ci", Note: "failure main", Symbol: SymbolUnwell}, UntilMs: now.Add(time.Minute).UnixMilli()})
	h := newsRow(l)

	alice := rowAs(t, h, tokenCaller("did:key:alice"))
	var seen []string
	for _, it := range alice.Items {
		if it.ID != "" {
			seen = append(seen, it.ID)
		}
	}
	if len(seen) != 1 || seen[0] != "n-alice" {
		t.Fatalf("alice sees %v; wanted only n-alice", seen)
	}

	bob := rowAs(t, h, tokenCaller("did:key:bob"))
	for _, it := range bob.Items {
		if it.ID == "n-alice" {
			t.Fatalf("bob was shown alice's news")
		}
	}

	// Alice at the browser: a session carries the identity and no grant.
	session := auth.Admitted(auth.LevelSuper)
	session.Identity = "https://mastodon.example/@alice"
	seenAtBrowser := false
	for _, it := range rowAs(t, h, session).Items {
		if it.ID == "n-alice" {
			seenAtBrowser = true
		}
	}
	if !seenAtBrowser {
		t.Fatal("alice's session was not shown what alice's token earned")
	}
}

// The item carries its id, so the laptop can write it down once and no more.
func TestNewsCarriesItsID(t *testing.T) {
	l := NewNewsLog()
	l.Leave(News{ID: "ground:payload:x", For: "https://mastodon.example/@alice", Item: StatusItem{Name: "ci", Symbol: SymbolWell}, UntilMs: time.Now().Add(time.Minute).UnixMilli()})
	body := rowAs(t, newsRow(l), tokenCaller("did:key:alice"))

	found := false
	for _, it := range body.Items {
		if it.ID == "ground:payload:x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the id did not reach the row: %+v", body.Items)
	}
}

// The hold is what lets a once-a-second poll not miss it. Past the hold it is
// gone from the row.
func TestNewsLeavesTheRowAfterItsHold(t *testing.T) {
	l := NewNewsLog()
	l.Leave(News{ID: "n-old", For: "https://mastodon.example/@alice", Item: StatusItem{Name: "ci", Symbol: SymbolWell}, UntilMs: time.Now().Add(-time.Second).UnixMilli()})
	body := rowAs(t, newsRow(l), tokenCaller("did:key:alice"))
	for _, it := range body.Items {
		if it.ID == "n-old" {
			t.Fatal("expired news is still on the row")
		}
	}
}

// A click on the item answers with the whole of it, by id.
func TestNewsItemAnswersByID(t *testing.T) {
	l := NewNewsLog()
	l.Leave(News{ID: "n-1", For: "https://mastodon.example/@alice", Item: StatusItem{Name: "ci", Note: "success main", Symbol: SymbolWell},
		Detail:  map[string]any{"repo": "teranos/ground", "conclusion": "success"},
		UntilMs: time.Now().Add(time.Minute).UnixMilli()})
	h := newsRow(l)

	req := httptest.NewRequest(http.MethodGet, "/am/statusline/n-1", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), tokenCaller("did:key:alice")))
	detail := itemDetail(t, h, req, "n-1")
	if detail["repo"] != "teranos/ground" || detail["conclusion"] != "success" {
		t.Fatalf("detail is %v", detail)
	}
}

// The tmux range carries the id when there is one, so a click on a news item
// asks for that item and not for whatever shares its name.
func TestRenderLineRangesUseTheID(t *testing.T) {
	line := renderLine([]StatusItem{{ID: "n-1", Name: "ci", Symbol: SymbolWell}}, palettes[FormatTmux], true)
	if want := "#[range=user|n-1]"; !contains(line, want) {
		t.Fatalf("line %q does not carry %q", line, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Tim: the tmux bar asks in its own format, and that is what the node remembers.
func TestTheRowRemembersWhenTmuxAsked(t *testing.T) {
	h := newsRow(NewNewsLog())
	for _, format := range []string{FormatTmux, FormatTmux, FormatJSON} {
		req := httptest.NewRequest(http.MethodGet, "/am/statusline?format="+format, nil)
		req = req.WithContext(auth.WithAdmission(req.Context(), tokenCaller("did:key:alice")))
		h.HandleStatusLine(httptest.NewRecorder(), req)
	}

	_, count, over, asked := h.Asked(alice)
	require.True(t, asked)
	assert.Equal(t, int64(2), count, "an ask in json is a browser's, not a bar's")
	var byTheMinute int64
	for _, n := range over {
		byTheMinute += n
	}
	assert.Equal(t, int64(2), byTheMinute)

	_, _, _, bobAsked := h.Asked("https://mastodon.example/@bob")
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
