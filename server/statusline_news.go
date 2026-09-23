package server

import (
	"sync"
	"time"

	"github.com/teranos/QNTX/server/auth"
)

// News is what a built-in leaves on the row for one caller. The row is polled
// once a second by a laptop that cannot be reached from here, so this is the
// return leg: the node holds the item until that poll has had time to see it.

// newsHold is how long an item stays on the row. The laptop polls each second
// and writes what it has not seen; two minutes is many polls, and a node that
// was unreachable for a minute still hands it over.
const newsHold = 2 * time.Minute

// Bounded: a handler leaving news in a tight loop must not grow this without end.
const newsLogSize = 256

// News is one item, for one caller, until one moment.
type News struct {
	// ID is the attestation's own, so the laptop writes it once whatever the
	// poll count. The row carries it and a click asks by it.
	ID string
	// For is who the item is addressed to: the token's DID that attested the
	// event, which is what the poll presents when it asks.
	For  string
	Item StatusItem
	// Detail is the whole of it, answered on a click.
	Detail  map[string]any
	UntilMs int64
	// Quiet is on the row and nowhere else: drawn without its id, so the
	// laptop that writes items down by id never writes this one and no
	// session is woken for it. What a built-in is doing, as against what it
	// concluded.
	Quiet bool
}

// newsLog holds recent news in memory. A restart empties it, which is correct:
// an item is a fact of this process having seen a run conclude.
type newsLog struct {
	mu    sync.Mutex
	items []News
}

func newNewsLog() *newsLog { return &newsLog{} }

// leave puts one item on the row. The same id twice is the newer one once.
func (l *newsLog) leave(n News) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.items {
		if l.items[i].ID == n.ID {
			l.items[i] = n
			return
		}
	}
	l.items = append(l.items, n)
	if len(l.items) > newsLogSize {
		l.items = l.items[len(l.items)-newsLogSize:]
	}
}

// drop takes an item off the row before its hold: what it said is over.
func (l *newsLog) drop(id string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.items {
		if l.items[i].ID == id {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return
		}
	}
}

// since is what is on the row for one caller now, oldest first.
func (l *newsLog) since(caller string, nowMs int64) []News {
	if l == nil || caller == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]News, 0, len(l.items))
	for _, n := range l.items {
		if n.For == caller && n.UntilMs > nowMs {
			out = append(out, n)
		}
	}
	return out
}

// byID is one item for one caller, held or not.
func (l *newsLog) byID(caller, id string) (News, bool) {
	if l == nil {
		return News{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.items {
		if n.ID == id && n.For == caller {
			return n, true
		}
	}
	return News{}, false
}

// newsKey is the name a caller's news is filed under: the person. A token
// speaks for whoever minted it (ADR-025) and carries that as Identity, and
// so does their session, so news left for a push one token attested is found
// by another of theirs, or by them at the browser. The DID is the key only
// for a caller that is nobody's.
func newsKey(a auth.Admission) string {
	if a.Identity != "" {
		return a.Identity
	}
	return a.ActsAs()
}

// newsFor is the caller's items as the row draws them.
func (h *StatusLineHandler) newsFor(a auth.Admission) []StatusItem {
	if h == nil || h.news == nil {
		return nil
	}
	held := h.news().since(newsKey(a), time.Now().UnixMilli())
	items := make([]StatusItem, 0, len(held))
	for _, n := range held {
		it := n.Item
		if !n.Quiet {
			it.ID = n.ID
		}
		items = append(items, it)
	}
	return items
}

// newsDetail answers /am/statusline/{id} for a news item.
func (h *StatusLineHandler) newsDetail(a auth.Admission, id string) (map[string]any, bool) {
	if h == nil || h.news == nil {
		return nil, false
	}
	n, ok := h.news().byID(newsKey(a), id)
	if !ok {
		return nil, false
	}
	detail := map[string]any{
		"id":      n.ID,
		"name":    n.Item.Name,
		"note":    n.Item.Note,
		"symbol":  n.Item.Symbol,
		"until":   time.UnixMilli(n.UntilMs).UTC().Format(time.RFC3339),
		"healthy": n.Item.Symbol == SymbolWell,
	}
	for k, v := range n.Detail {
		detail[k] = v
	}
	return detail, true
}
