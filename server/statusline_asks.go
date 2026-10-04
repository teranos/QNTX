package server

import (
	"sync"
	"time"
)

// "this seems like a QNTX change from the backend side, have we tackled this yet?"

// ug tmux is the one surface that asks for the row in tmux's own escapes
// (ground ug/tmux.d), so an ask in that format is a tmux bar polling.

// How many minutes of asks are kept, a count a minute: what a sparkline of the
// last hour is drawn from.
const rowAskMinutes = 60

// rowAsk is when one person's tmux bar last asked, how often since this
// process began, and how often in each of the last minutes.
type rowAsk struct {
	LastMs  int64
	Count   int64
	minutes map[int64]int64
}

// rowAsks holds that per person, in memory. A restart empties it: the node
// reports the bars it has answered, not the ones it once did.
type rowAsks struct {
	mu sync.Mutex
	by map[string]*rowAsk
}

func newRowAsks() *rowAsks { return &rowAsks{by: map[string]*rowAsk{}} }

// note records one ask by one person. Nobody asking is nobody's bar.
func (a *rowAsks) note(who string, at time.Time) {
	if a == nil || who == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	asked, seen := a.by[who]
	if !seen {
		asked = &rowAsk{minutes: map[int64]int64{}}
		a.by[who] = asked
	}
	asked.LastMs = at.UnixMilli()
	asked.Count++
	minute := at.Unix() / 60
	asked.minutes[minute]++
	for m := range asked.minutes {
		if m <= minute-rowAskMinutes {
			delete(asked.minutes, m)
		}
	}
}

// of is what one person's bar has asked: the last, the count, and the asks of
// each minute still kept, by the minute as `2026-10-03T22:47` in UTC. False is
// a person whose bar never asked.
func (a *rowAsks) of(who string) (lastMs, count int64, over map[string]int64, asked bool) {
	if a == nil || who == "" {
		return 0, 0, nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	held, ok := a.by[who]
	if !ok {
		return 0, 0, nil, false
	}
	over = make(map[string]int64, len(held.minutes))
	for minute, n := range held.minutes {
		over[time.Unix(minute*60, 0).UTC().Format("2006-01-02T15:04")] = n
	}
	return held.LastMs, held.Count, over, true
}
