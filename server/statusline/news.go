package statusline

import (
	"sort"
	"sync"
	"time"

	"github.com/teranos/QNTX/server/auth"
)

// News is what a built-in leaves on the row for one caller. The row is polled
// once a second by a laptop that cannot be reached from here, so this is the
// return leg: the node holds the item until that poll has had time to see it.

// NewsHold is how long an item stays on the row. The laptop polls each second
// and writes what it has not seen; two minutes is many polls, and a node that
// was unreachable for a minute still hands it over.
const NewsHold = 2 * time.Minute

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
	// AtMs is when it was left, stamped by the log.
	AtMs int64
}

// NewsLog holds recent news in memory. A restart empties it, which is correct:
// an item is a fact of this process having seen a run conclude.
type NewsLog struct {
	mu    sync.Mutex
	items []News
	// left is how many conclusions were left for each addressee since this
	// process began. A wait is not one: it is left again on every turn of its loop.
	left map[string]int64
}

func NewNewsLog() *NewsLog { return &NewsLog{left: map[string]int64{}} }

// leave puts one item on the row. The same id twice is the newer one once.
func (l *NewsLog) Leave(n News) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n.AtMs = time.Now().UnixMilli()
	if !n.Quiet {
		l.left[n.For]++
	}
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
func (l *NewsLog) Drop(id string) {
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
func (l *NewsLog) Since(caller string, nowMs int64) []News {
	if caller == "" {
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

// LeftFor is everything the log still holds for one caller, newest first, on
// the row or past its hold, and how many conclusions it left for them since
// this process began. The log is bounded, so the count can pass what it holds.
func (l *NewsLog) LeftFor(caller string) ([]News, int64) {
	if caller == "" {
		return nil, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]News, 0, len(l.items))
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].For == caller {
			out = append(out, l.items[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AtMs > out[j].AtMs })
	return out, l.left[caller]
}

// ByID is one item for one caller, held or not.
func (l *NewsLog) ByID(caller, id string) (News, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.items {
		if n.ID == id && n.For == caller {
			return n, true
		}
	}
	return News{}, false
}

// NewsKey is the name a caller's news is filed under: the person. A token
// speaks for whoever minted it (ADR-025) and carries that as Identity, and
// so does their session, so news left for a push one token attested is found
// by another of theirs, or by them at the browser. The DID is the key only
// for a caller that is nobody's.
func NewsKey(a auth.Admission) string {
	if a.Identity != "" {
		return a.Identity
	}
	return a.ActsAs()
}

// newsFor is the caller's items as the row draws them.
func (h *StatusLineHandler) NewsFor(a auth.Admission) []StatusItem {
	return newsItems(h.heldNews(a))
}

// heldNews is what is on the row for the caller now.
func (h *StatusLineHandler) heldNews(a auth.Admission) []News {
	if h == nil || h.News == nil {
		return nil
	}
	return h.News().Since(NewsKey(a), time.Now().UnixMilli())
}

// newsItems is news as the row draws it.
func newsItems(held []News) []StatusItem {
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
	if h == nil || h.News == nil {
		return nil, false
	}
	n, ok := h.News().ByID(NewsKey(a), id)
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
