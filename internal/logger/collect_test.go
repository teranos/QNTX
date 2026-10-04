package logger

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func collected(t *testing.T, level zapcore.Level) (*zap.Logger, *observer.ObservedLogs, *clock) {
	t.Helper()
	inner, logs := observer.New(level)
	at := &clock{at: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	return zap.New(newCollector(inner, at.now)), logs, at
}

func occurrences(logs *observer.ObservedLogs) []int64 {
	var got []int64
	for _, entry := range logs.AllUntimed() {
		n, ok := entry.ContextMap()["occurrences"].(int64)
		if !ok {
			n = 1
		}
		got = append(got, n)
	}
	return got
}

func TestARepeatedLineIsWrittenAtItsFirstAndEveryPowerOfFour(t *testing.T) {
	log, logs, _ := collected(t, zapcore.InfoLevel)
	for range 70 {
		log.Error("failed to enqueue execution for watcher w1", zap.String("error", "exec failed"))
	}

	got := occurrences(logs)
	want := []int64{1, 4, 16, 64}
	if len(got) != len(want) {
		t.Fatalf("written occurrences %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("written occurrences %v, want %v", got, want)
		}
	}
}

func TestALineNamingAnotherSubjectIsItsOwnLine(t *testing.T) {
	log, logs, _ := collected(t, zapcore.InfoLevel)
	sub := log.With(zap.String("plugin", "inbox"))
	for range 3 {
		log.Warn("plugin did not answer", zap.String("plugin", "capy"))
		sub.Warn("plugin did not answer")
	}

	if n := logs.Len(); n != 2 {
		t.Fatalf("%d lines written, want the first of each subject: 2", n)
	}
}

func TestALineAwayForTheQuietWindowStartsCountingAgain(t *testing.T) {
	log, logs, at := collected(t, zapcore.InfoLevel)
	log.Error("operational store has not answered")
	log.Error("operational store has not answered")
	at.at = at.at.Add(collectQuiet)
	log.Error("operational store has not answered")

	got := occurrences(logs)
	if len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("written occurrences %v, want [1 1]", got)
	}
}

func TestEachSinkStillTakesOnlyItsOwnLevels(t *testing.T) {
	infoCore, infoLogs := observer.New(zapcore.InfoLevel)
	errorCore, errorLogs := observer.New(zapcore.ErrorLevel)
	at := &clock{at: time.Now()}
	log := zap.New(newCollector(zapcore.NewTee(infoCore, errorCore), at.now))

	log.Info("started")
	log.Error("stopped")

	if infoLogs.Len() != 2 || errorLogs.Len() != 1 {
		t.Fatalf("info sink took %d, error sink took %d; want 2 and 1", infoLogs.Len(), errorLogs.Len())
	}
}

func TestIsPowerOfFour(t *testing.T) {
	for n, want := range map[int]bool{1: true, 2: false, 4: true, 8: false, 12: false, 16: true, 64: true, 256: true, 255: false} {
		if got := isPowerOfFour(n); got != want {
			t.Errorf("isPowerOfFour(%d) = %t, want %t", n, got, want)
		}
	}
}
