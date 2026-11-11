package logger

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// "WE DONT NEED TO SEND 3000 ERRORS"
//
// "WE NEED TO CAP THE AMOUNT OR ERRORS SENT TO SENTRY AND TRACK IT IN QNTX."
//
// "WE NEED OUR OWN ABSTRACTION AND TRACKING OF THIS IN CASE WE DO HIT SENTRY LIMIT, SO WE NEVER REALLY LOST OBS"
//
// See docs/sentry.md.

// issuesPerDay is the most issues the node raises to Sentry in one UTC day.
const issuesPerDay = 100

// Issue is one issue's day: what the node counted, and what of it went to Sentry.
// Occurrences minus Raised is what Sentry did not get.
type Issue struct {
	Day         string // UTC, 2006-01-02
	Key         string // sha256 of level, logger, caller, message and plugin
	Level       string
	Logger      string
	Caller      string
	Message     string
	Plugin      string
	LastError   string
	FirstSeen   time.Time
	LastSeen    time.Time
	Occurrences int
	Raised      int
}

// issues is the node's ledger. The collector counts into it and the Sentry core
// asks it before raising.
var issues = newIssueLedger(time.Now, issuesPerDay)

// TakeIssues hands over every issue that changed since the last call.
func TakeIssues() []Issue { return issues.take() }

// SeedIssues takes in what the node already kept of today, before anything is
// counted: a restart within a UTC day continues its counts, its raises and the
// day's cap, and does not write smaller totals over larger ones.
func SeedIssues(kept []Issue) { issues.seed(kept) }

// ReturnIssues gives back issues that were taken and not written, so the next
// take hands them over again.
func ReturnIssues(taken []Issue) { issues.giveBack(taken) }

type issueLedger struct {
	mu       sync.Mutex
	now      func() time.Time
	perDay   int
	day      string
	raised   int // raised today, across all issues
	today    map[string]*Issue
	changed  map[string]*Issue // since the last take, by day+key; holds yesterday's until taken
	nextRise map[string]int    // today's occurrence count at which an issue is next raised
}

func newIssueLedger(now func() time.Time, perDay int) *issueLedger {
	return &issueLedger{
		now:      now,
		perDay:   perDay,
		today:    map[string]*Issue{},
		changed:  map[string]*Issue{},
		nextRise: map[string]int{},
	}
}

// seen counts one occurrence of the issue this entry is.
func (l *issueLedger) seen(ent zapcore.Entry, fields []zapcore.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	issue := l.issueFor(ent, fields)
	issue.Occurrences++
	issue.LastSeen = l.now()
	if err := firstError(fields); err != nil {
		issue.LastError = err.Error()
	}
}

// raise says whether this entry goes to Sentry as an issue: the 1st, 4th,
// 16th… occurrence of the issue today, while the day's cap is not reached.
func (l *issueLedger) raise(ent zapcore.Entry, fields []zapcore.Field) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	issue := l.issueFor(ent, fields)
	if issue.Occurrences == 0 {
		// Nothing counted it: no collector is in front of this core.
		issue.Occurrences = 1
		issue.LastSeen = l.now()
	}
	next := l.nextRise[issue.Key]
	if issue.Occurrences < next || l.raised >= l.perDay {
		return false
	}
	l.raised++
	issue.Raised++
	l.nextRise[issue.Key] = max(issue.Occurrences, 1) * 4
	l.changed[issue.Day+issue.Key] = issue
	return true
}

func (l *issueLedger) seed(kept []Issue) {
	l.mu.Lock()
	defer l.mu.Unlock()
	day := l.now().UTC().Format(time.DateOnly)
	if day != l.day {
		l.day, l.raised = day, 0
		l.today = map[string]*Issue{}
		l.nextRise = map[string]int{}
	}
	for _, k := range kept {
		if k.Day != day {
			continue
		}
		if held, counted := l.today[k.Key]; counted {
			// Counted before the seed: the kept totals are added under it.
			held.Occurrences += k.Occurrences
			held.Raised += k.Raised
			held.FirstSeen = k.FirstSeen
		} else {
			issue := k
			l.today[k.Key] = &issue
		}
		l.raised += k.Raised
		if k.Raised > 0 {
			l.nextRise[k.Key] = max(k.Occurrences, 1) * 4
		}
	}
}

// take hands over what changed since the last take, in day and key order.
func (l *issueLedger) take() []Issue {
	l.mu.Lock()
	defer l.mu.Unlock()
	taken := make([]Issue, 0, len(l.changed))
	for _, issue := range l.changed {
		taken = append(taken, *issue)
	}
	clear(l.changed)
	sort.Slice(taken, func(i, j int) bool {
		if taken[i].Day != taken[j].Day {
			return taken[i].Day < taken[j].Day
		}
		return taken[i].Key < taken[j].Key
	})
	return taken
}

// giveBack marks taken issues changed again. One that changed since it was
// taken is already marked, and its newer counts stand.
func (l *issueLedger) giveBack(taken []Issue) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, issue := range taken {
		if _, marked := l.changed[issue.Day+issue.Key]; marked {
			continue
		}
		back := issue
		l.changed[issue.Day+issue.Key] = &back
	}
}

// issueFor finds or starts today's issue for this entry, and marks it changed.
func (l *issueLedger) issueFor(ent zapcore.Entry, fields []zapcore.Field) *Issue {
	now := l.now()
	if day := now.UTC().Format(time.DateOnly); day != l.day {
		l.day, l.raised = day, 0
		l.today = map[string]*Issue{}
		l.nextRise = map[string]int{}
	}

	plugin := pluginOf(fields)
	parts := []string{ent.Level.String(), ent.LoggerName, ent.Caller.TrimmedPath(), ent.Message, plugin}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	key := hex.EncodeToString(sum[:])

	issue, ok := l.today[key]
	if !ok {
		issue = &Issue{
			Day:       l.day,
			Key:       key,
			Level:     ent.Level.String(),
			Logger:    ent.LoggerName,
			Caller:    ent.Caller.TrimmedPath(),
			Message:   ent.Message,
			Plugin:    plugin,
			FirstSeen: now,
		}
		l.today[key] = issue
	}
	l.changed[issue.Day+key] = issue
	return issue
}

// pluginOf is the "plugin" field, the subject the fingerprint splits on.
func pluginOf(fields []zapcore.Field) string {
	for _, field := range fields {
		if field.Key == "plugin" && field.Type == zapcore.StringType {
			return field.String
		}
	}
	return ""
}
