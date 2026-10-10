package server

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// "THE STARS, QNTX, IT CAN ONLY REALLY TELL YOU ABOUT SOME MINOR THINGS, LIKE HOW MANY TIMES IT WROTE DEFERRED NEW BACK OR ITS OWN UPTIME"

// "this part is supposed to show things QNTX does for ground specifically"

// What the node does in Ground's place is ci.watch: it waits on a run where the
// socket is, and leaves what it concluded on the status line the laptop polls
// (ci_pulse.go). am ground is that, asked for whole.

// groundWatches is every standing watcher that reaches ci.watch: what Ground
// attests that sets the node waiting.
func groundWatches() ([]map[string]any, error) {
	watches := []map[string]any{}
	for _, w := range watcher.Standing() {
		if w.ActionType != storage.ActionTypeBuiltinExecute {
			continue
		}
		var action watcher.BuiltinExecuteAction
		if err := json.Unmarshal([]byte(w.ActionData), &action); err != nil {
			return nil, errors.Wrapf(err, "the standing watcher %s names a built-in in action data that does not read: %q", w.ID, w.ActionData)
		}
		if action.HandlerName != watcher.CIWatchHandlerName {
			continue
		}
		watches = append(watches, map[string]any{"id": w.ID, "name": w.Name, "predicates": w.Filter.Predicates})
	}
	return watches, nil
}

// What ug's status line posts for its session, and who it posts as (ground
// ug/usage.d attestationInto).
const (
	ugReadingPredicate = "rate_limit"
	ugActor            = "ug"
)

// How far back a session's readings are looked for, and how many are read.
const (
	ugReadingsWindow = 24 * time.Hour
	ugReadingsLimit  = 1000
)

// An hour as a bucket's key, in UTC: what a sparkline of a day is drawn from.
const hourBucket = "2006-01-02T15"

// ugReadings is what ug's status lines posted since the moment given, read two
// ways: each session that posted, the latest first, with its readings by the
// hour; and each window ug reads, with what it read over time, oldest first.
func ugReadings(store ats.AttestationStore, since time.Time) (sessions, windows []map[string]any, err error) {
	found, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{ugReadingPredicate},
		Actors:     []string{ugActor},
		TimeStart:  &since,
		Limit:      ugReadingsLimit,
	})
	if err != nil {
		return nil, nil, errors.Wrapf(err, "reading the %s rows %s posted since %s", ugReadingPredicate, ugActor, since.UTC().Format(time.RFC3339))
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Timestamp.Before(found[j].Timestamp) })

	type drew struct {
		session     string
		first, last time.Time
		readings    int
		over        map[string]int
	}
	bySession := map[string]*drew{}
	byWindow := map[string][]map[string]any{}
	for _, as := range found {
		// A store's filter is broader than equality (transcripts.go), and not
		// every backend honours TimeStart (ci_pulse.go), so both are held here.
		if len(as.Predicates) == 0 || as.Predicates[0] != ugReadingPredicate || !slices.Contains(as.Actors, ugActor) || as.Timestamp.Before(since) {
			continue
		}
		at := as.Timestamp.UTC()
		// The subject is the window read, and used_percentage what it read (ug/usage.d).
		if used, ok := as.Attributes["used_percentage"].(float64); ok && len(as.Subjects) > 0 {
			window := as.Subjects[0]
			byWindow[window] = append(byWindow[window], map[string]any{"at": at.Format(time.RFC3339), "used": used})
		}
		session := sessionOf(as.Contexts)
		if session == "" {
			continue
		}
		d, seen := bySession[session]
		if !seen {
			d = &drew{session: session, first: at, over: map[string]int{}}
			bySession[session] = d
		}
		d.last = at
		d.readings++
		d.over[at.Format(hourBucket)]++
	}

	drawn := make([]*drew, 0, len(bySession))
	for _, d := range bySession {
		drawn = append(drawn, d)
	}
	sort.Slice(drawn, func(i, j int) bool {
		if !drawn[i].last.Equal(drawn[j].last) {
			return drawn[i].last.After(drawn[j].last)
		}
		return drawn[i].session < drawn[j].session
	})
	sessions = make([]map[string]any, 0, len(drawn))
	for _, d := range drawn {
		sessions = append(sessions, map[string]any{
			"session":  d.session,
			"first":    d.first.Format(time.RFC3339),
			"last":     d.last.Format(time.RFC3339),
			"readings": d.readings,
			"over":     d.over,
		})
	}

	named := make([]string, 0, len(byWindow))
	for window := range byWindow {
		named = append(named, window)
	}
	sort.Strings(named)
	windows = make([]map[string]any, 0, len(named))
	for _, window := range named {
		windows = append(windows, map[string]any{"window": window, "readings": byWindow[window]})
	}
	return sessions, windows, nil
}

// amGround answers am ground for whoever asks. News is filed under the person
// (newsKey), so a node that knows nobody has left nothing for anybody.
func (s *QNTXServer) amGround(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	watches, err := groundWatches()
	if err != nil {
		s.logger.Errorw("am ground could not read what the node watches for Ground", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what the node watches for Ground did not read: " + err.Error()}
	}

	var held []News
	var left int64
	tmux := map[string]any{"asked": int64(0), "last": "", "over": map[string]int64{}}
	if admitted, ok := auth.AdmissionFrom(ctx); ok {
		held, left = s.news.leftFor(newsKey(admitted))
		if s.statusLineHandler != nil {
			if lastMs, count, over, asked := s.statusLineHandler.tmux.of(newsKey(admitted)); asked {
				tmux = map[string]any{"asked": count, "last": time.UnixMilli(lastMs).UTC().Format(time.RFC3339), "over": over}
			}
		}
	}

	// ug's readings are rows in the namespace the caller stands in. Asked
	// outside a request there is no namespace to read them from.
	since := time.Now().Add(-ugReadingsWindow)
	sessions, windows := []map[string]any{}, []map[string]any{}
	if caller := sigil.Caller(ctx); caller != nil {
		store, err := s.storeFor(caller)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no namespace to read ug's readings from: " + err.Error()}
		}
		sessions, windows, err = ugReadings(store, since)
		if err != nil {
			s.logger.Errorw("am ground could not read what ug posted", "error", err)
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what ug posted did not read: " + err.Error()}
		}
	}
	now := time.Now().UnixMilli()
	news := make([]map[string]any, 0, len(held))
	for _, n := range held {
		said := map[string]any{}
		// What the item carries in full, the way a click on it answers (newsDetail).
		for k, v := range n.Detail {
			said[k] = v
		}
		said["id"] = n.ID
		said["name"] = n.Item.Name
		said["note"] = n.Item.Note
		said["symbol"] = n.Item.Symbol
		said["waiting"] = n.Quiet
		said["at"] = time.UnixMilli(n.AtMs).UTC().Format(time.RFC3339)
		said["until"] = time.UnixMilli(n.UntilMs).UTC().Format(time.RFC3339)
		said["on_row"] = n.UntilMs > now
		news = append(news, said)
	}

	failed := []map[string]any{}
	for _, f := range s.handlerFailures.since(handlerFailureWindow) {
		if f.Handler != watcher.CIWatchHandlerName {
			continue
		}
		failed = append(failed, map[string]any{
			"at":           time.UnixMilli(f.AtMs).UTC().Format(time.RFC3339),
			"error":        f.Error,
			"execution_id": f.ExecutionID,
		})
	}

	started := ""
	if !s.startedAt.IsZero() {
		started = s.startedAt.UTC().Format(time.RFC3339)
	}
	ug := map[string]any{"tmux": tmux, "sessions": sessions, "windows": windows, "since": since.UTC().Format(time.RFC3339)}
	return map[string]any{"started": started, "left": left, "watches": watches, "news": news, "failed": failed, "ug": ug}, nil
}
