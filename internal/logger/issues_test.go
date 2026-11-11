package logger

import (
	"fmt"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// The line that sent 3,103 issue events to Sentry on 2026-11-11.
func forwardFailed() zapcore.Entry {
	return zapcore.Entry{
		Level:      zapcore.ErrorLevel,
		LoggerName: "server.forward",
		Message:    "A delivery did not reach the node it is forwarded to",
	}
}

func forwardFields(delivery int) []zapcore.Field {
	return []zapcore.Field{
		zap.String("url", "http://10.88.0.2:8770/github/webhook"),
		zap.String("delivery", fmt.Sprintf("delivery-%d", delivery)),
		zap.Error(errors.New("dial tcp 10.88.0.2:8770: i/o timeout")),
	}
}

type fixedClock struct{ at time.Time }

func (c *fixedClock) now() time.Time { return c.at }

func ledgerAt(at time.Time, perDay int) (*issueLedger, *fixedClock) {
	clock := &fixedClock{at: at}
	return newIssueLedger(clock.now, perDay), clock
}

// "WE DONT NEED TO SEND 3000 ERRORS"
//
// A field that differs per occurrence does not make a new issue, and one issue
// is raised on its 1st, 4th, 16th… occurrence.
func TestOneIssueIsRaisedOnPowersOfFour(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)

	raised := 0
	for i := range 3000 {
		ledger.seen(forwardFailed(), forwardFields(i))
		if ledger.raise(forwardFailed(), forwardFields(i)) {
			raised++
		}
	}
	// 1, 4, 16, 64, 256, 1024
	if raised != 6 {
		t.Errorf("3000 occurrences raised %d issues, want 6", raised)
	}

	kept := ledger.take()
	if len(kept) != 1 {
		t.Fatalf("the ledger holds %d issues, want 1: %+v", len(kept), kept)
	}
	if kept[0].Occurrences != 3000 || kept[0].Raised != 6 {
		t.Errorf("kept occurrences=%d raised=%d, want 3000 and 6", kept[0].Occurrences, kept[0].Raised)
	}
	if kept[0].LastError != "dial tcp 10.88.0.2:8770: i/o timeout" {
		t.Errorf("kept last error %q", kept[0].LastError)
	}
	if kept[0].Day != "2026-11-11" {
		t.Errorf("kept day %q, want 2026-11-11", kept[0].Day)
	}
}

// "WE NEED TO CAP THE AMOUNT OR ERRORS SENT TO SENTRY AND TRACK IT IN QNTX."
//
// Past the day's cap nothing is raised, and every occurrence is still counted.
func TestTheDayIsCapped(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 2)

	for i, message := range []string{"one", "two", "three"} {
		ent := zapcore.Entry{Level: zapcore.ErrorLevel, Message: message}
		ledger.seen(ent, nil)
		if got, want := ledger.raise(ent, nil), i < 2; got != want {
			t.Errorf("issue %q raised=%v, want %v", message, got, want)
		}
	}

	kept := ledger.take()
	if len(kept) != 3 {
		t.Fatalf("the ledger holds %d issues, want 3", len(kept))
	}
	for _, issue := range kept {
		if issue.Occurrences != 1 {
			t.Errorf("issue %q occurrences=%d, want 1", issue.Message, issue.Occurrences)
		}
		if issue.Message == "three" && issue.Raised != 0 {
			t.Errorf("the issue past the cap was raised %d times", issue.Raised)
		}
	}
}

// A new UTC day is a new count and a new cap.
func TestANewDayStartsOver(t *testing.T) {
	ledger, clock := ledgerAt(time.Date(2026, 11, 11, 23, 59, 0, 0, time.UTC), 1)

	ent := zapcore.Entry{Level: zapcore.ErrorLevel, Message: "the store is gone"}
	ledger.seen(ent, nil)
	if !ledger.raise(ent, nil) {
		t.Fatal("the first occurrence of the day was not raised")
	}
	ledger.seen(ent, nil)
	ledger.raise(ent, nil)

	clock.at = clock.at.Add(2 * time.Minute)
	ledger.seen(ent, nil)
	if !ledger.raise(ent, nil) {
		t.Error("the first occurrence of the next day was not raised")
	}

	days := map[string]int{}
	for _, issue := range ledger.take() {
		days[issue.Day] = issue.Occurrences
	}
	if days["2026-11-11"] != 2 || days["2026-11-12"] != 1 {
		t.Errorf("occurrences by day = %v, want 2026-11-11:2 2026-11-12:1", days)
	}
}

// take hands over what changed since the last take, so a flush writes only that.
func TestTakeHandsOverOnlyWhatChanged(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)

	ledger.seen(zapcore.Entry{Level: zapcore.ErrorLevel, Message: "one"}, nil)
	ledger.seen(zapcore.Entry{Level: zapcore.ErrorLevel, Message: "two"}, nil)
	if got := len(ledger.take()); got != 2 {
		t.Fatalf("the first take handed over %d, want 2", got)
	}
	if got := len(ledger.take()); got != 0 {
		t.Errorf("a take with nothing changed handed over %d, want 0", got)
	}
	ledger.seen(zapcore.Entry{Level: zapcore.ErrorLevel, Message: "two"}, nil)
	kept := ledger.take()
	if len(kept) != 1 || kept[0].Message != "two" || kept[0].Occurrences != 2 {
		t.Errorf("after one more occurrence the take handed over %+v", kept)
	}
}

// The plugin field is the subject, the same as in the fingerprint: two plugins
// failing are two issues.
func TestThePluginFieldSplitsTheIssue(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)

	ent := zapcore.Entry{Level: zapcore.ErrorLevel, Message: "Plugin failed consecutive health checks, restarting"}
	ledger.seen(ent, []zapcore.Field{zap.String("plugin", "duif")})
	ledger.seen(ent, []zapcore.Field{zap.String("plugin", "otaripy")})
	if got := len(ledger.take()); got != 2 {
		t.Errorf("two plugins failing are %d issues, want 2", got)
	}
}

// The collector counts every occurrence into the ledger, also the ones it folds.
func TestTheCollectorCountsWhatItFolds(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)
	inner, _ := coreWith(t, zapcore.DebugLevel)
	c := newCollector(inner, time.Now)
	c.issues = ledger

	ent := zapcore.Entry{Level: zapcore.ErrorLevel, Message: "the store is gone"}
	for range 10 {
		if err := c.Write(ent, nil); err != nil {
			t.Fatalf("Write returned %v", err)
		}
	}
	// Below error nothing is an issue.
	if err := c.Write(zapcore.Entry{Level: zapcore.WarnLevel, Message: "slow"}, nil); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	kept := ledger.take()
	if len(kept) != 1 || kept[0].Occurrences != 10 {
		t.Errorf("the ledger kept %+v, want one issue with 10 occurrences", kept)
	}
}

// The Sentry core asks the ledger before it raises.
func TestSentryRaisesWhatTheLedgerLets(t *testing.T) {
	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://tim@example.com/1", Transport: transport}); err != nil {
		t.Fatalf("sentry.Init returned %v", err)
	}
	defer func() { sentry.CurrentHub().BindClient(nil) }()

	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)
	core, _ := coreWith(t, zapcore.InfoLevel)
	core.captureErrors = true
	core.issues = ledger

	for i := range 20 {
		ledger.seen(forwardFailed(), forwardFields(i))
		if err := core.Write(forwardFailed(), forwardFields(i)); err != nil {
			t.Fatalf("Write returned %v", err)
		}
	}
	// 1, 4, 16
	if got := len(transport.Events()); got != 3 {
		t.Errorf("20 occurrences raised %d events, want 3", got)
	}
}

// What was taken and not written comes back on the next take.
func TestGivenBackIsTakenAgain(t *testing.T) {
	ledger, _ := ledgerAt(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), 100)

	ledger.seen(zapcore.Entry{Level: zapcore.ErrorLevel, Message: "one"}, nil)
	ledger.giveBack(ledger.take())
	kept := ledger.take()
	if len(kept) != 1 || kept[0].Message != "one" {
		t.Errorf("after giving back, the take handed over %+v", kept)
	}
}
