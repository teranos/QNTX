//go:build cgo

package sqlitecgo

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/logger"
)

// slowOpThreshold is the duration above which FFI operations are collected.
const slowOpThreshold = 2 * time.Second

// flushInterval controls how often collected stats are flushed to the log.
const flushInterval = 5 * time.Minute

// logSlowOp records an FFI operation that exceeded the threshold.
// Slow ops are collected and flushed as summaries, not logged individually.
func logSlowOp(start time.Time, op string) {
	elapsed := time.Since(start)
	if elapsed < slowOpThreshold {
		return
	}
	slowOpCount.Add(1)
	collector.record(op, elapsed)
}

// mutexWaitThreshold is the duration above which mutex waits are collected.
// Higher than slowOpThreshold because moderate contention is expected under load.
const mutexWaitThreshold = 5 * time.Second

// recordWait records a mutex-wait duration. Only records waits above the threshold.
func recordWait(start time.Time, mutex string) {
	elapsed := time.Since(start)
	if elapsed < mutexWaitThreshold {
		return
	}
	collector.record("mutex:"+mutex, elapsed)
}

const historySize = 60 // 60 x 5min = 5 hours of rolling history

// slowCollector accumulates all slow events (ops and waits) into buckets
// keyed by operation name. Flushes summaries periodically, suppressing
// repeats that haven't changed. Keeps a rolling history of snapshots.
type slowCollector struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	lastFlush map[string]bucketSnapshot // previous flush for change detection
	history   [historySize]map[string]*BucketStats
	histIdx   int
	flushOnce sync.Once
}

type bucket struct {
	count int
	min   time.Duration
	max   time.Duration
	total time.Duration
}

type bucketSnapshot struct {
	count int
	avg   time.Duration
}

// BucketStats is the exported snapshot of a single bucket.
type BucketStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
}

// PerformanceSnapshot is the exported rolling history of slow ops and mutex waits.
type PerformanceSnapshot struct {
	// Current is the latest flushed window (may be nil if no flush yet).
	Current map[string]*BucketStats
	// History is the last N windows (oldest first), each keyed by operation name.
	History []map[string]*BucketStats
}

var collector = &slowCollector{
	buckets:   make(map[string]*bucket),
	lastFlush: make(map[string]bucketSnapshot),
}

func (sc *slowCollector) record(key string, elapsed time.Duration) {
	sc.mu.Lock()
	b, ok := sc.buckets[key]
	if !ok {
		b = &bucket{min: elapsed, max: elapsed}
		sc.buckets[key] = b
	}
	b.count++
	b.total += elapsed
	if elapsed < b.min {
		b.min = elapsed
	}
	if elapsed > b.max {
		b.max = elapsed
	}
	sc.mu.Unlock()

	sc.flushOnce.Do(func() {
		go sc.flusher()
	})
}

func (sc *slowCollector) flusher() {
	for {
		time.Sleep(flushInterval)
		sc.flush()
	}
}

func (sc *slowCollector) flush() {
	sc.mu.Lock()
	snapshot := sc.buckets
	sc.buckets = make(map[string]*bucket)

	// Store in ring buffer
	exported := make(map[string]*BucketStats, len(snapshot))
	for key, b := range snapshot {
		avg := b.total / time.Duration(b.count)
		exported[key] = &BucketStats{
			Count: b.count,
			Min:   b.min,
			Max:   b.max,
			Avg:   avg,
		}
	}
	sc.history[sc.histIdx%historySize] = exported
	sc.histIdx++
	sc.mu.Unlock()

	for key, b := range snapshot {
		avg := b.total / time.Duration(b.count)
		prev, seen := sc.lastFlush[key]

		// Suppress if count and avg are roughly the same as last flush.
		if seen && b.count == prev.count && durSimilar(avg, prev.avg) {
			continue
		}

		sc.lastFlush[key] = bucketSnapshot{count: b.count, avg: avg}

		if name, ok := strings.CutPrefix(key, "mutex:"); ok {
			logger.Logger.Infow("Mutex contention (5m)",
				"mutex", name,
				"waiters", b.count,
				"min", b.min,
				"max", b.max,
				"avg", avg,
			)
		} else {
			logger.Logger.Infow("Slow ops (5m)",
				"op", key,
				"count", b.count,
				"min", b.min,
				"max", b.max,
				"avg", avg,
			)
		}
	}
}

// Snapshot returns the rolling performance history. Safe for concurrent use.
func (sc *slowCollector) Snapshot() PerformanceSnapshot {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	var current map[string]*BucketStats
	var history []map[string]*BucketStats

	n := sc.histIdx
	n = min(n, historySize)

	for i := 0; i < n; i++ {
		idx := (sc.histIdx - n + i) % historySize
		if sc.history[idx] != nil {
			history = append(history, sc.history[idx])
		}
	}

	if len(history) > 0 {
		current = history[len(history)-1]
	}

	return PerformanceSnapshot{
		Current: current,
		History: history,
	}
}

// GetPerformanceSnapshot returns the current rolling performance data.
func GetPerformanceSnapshot() PerformanceSnapshot {
	return collector.Snapshot()
}

// durSimilar returns true if two durations are within 20% of each other.
func durSimilar(a, b time.Duration) bool {
	if a == 0 && b == 0 {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	larger := max(a, b)
	return diff <= larger/5
}

// slowQueryKey builds a stable key from a query filter, stripping volatile
// fields (timestamps, limit) so repeated queries collapse in the collector.
func slowQueryKey(f ats.AttestationFilter) string {
	var parts []string
	if len(f.Subjects) > 0 {
		parts = append(parts, "s="+strings.Join(f.Subjects, ","))
	}
	if len(f.Predicates) > 0 {
		parts = append(parts, "p="+strings.Join(f.Predicates, ","))
	}
	if len(f.Contexts) > 0 {
		parts = append(parts, "c="+strings.Join(f.Contexts, ","))
	}
	if len(f.Actors) > 0 {
		parts = append(parts, "a="+strings.Join(f.Actors, ","))
	}
	if f.Source != "" {
		parts = append(parts, "src="+f.Source)
	}
	if f.TimeStart != nil {
		parts = append(parts, "t=window")
	}
	if len(parts) == 0 {
		return "unfiltered"
	}
	return strings.Join(parts, " ")
}

// slowOpCount tracks total slow operations for observability.
var slowOpCount atomic.Int64

// SlowOpCount returns the total number of slow operations recorded.
func SlowOpCount() int64 {
	return slowOpCount.Load()
}
