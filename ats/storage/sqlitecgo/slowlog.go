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
const slowOpThreshold = 500 * time.Millisecond

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

// recordWait records a mutex-wait duration. Only records waits above the threshold.
func recordWait(start time.Time, mutex string) {
	elapsed := time.Since(start)
	if elapsed < slowOpThreshold {
		return
	}
	collector.record("mutex:"+mutex, elapsed)
}

// slowCollector accumulates all slow events (ops and waits) into buckets
// keyed by operation name. Flushes summaries periodically, suppressing
// repeats that haven't changed.
type slowCollector struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	lastFlush map[string]bucketSnapshot // previous flush for change detection
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
	sc.mu.Unlock()

	for key, b := range snapshot {
		avg := b.total / time.Duration(b.count)
		prev, seen := sc.lastFlush[key]

		// Suppress if count and avg are roughly the same as last flush.
		if seen && b.count == prev.count && durSimilar(avg, prev.avg) {
			continue
		}

		sc.lastFlush[key] = bucketSnapshot{count: b.count, avg: avg}

		if strings.HasPrefix(key, "mutex:") {
			logger.Logger.Infow("Mutex contention (5m)",
				"mutex", strings.TrimPrefix(key, "mutex:"),
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

// durSimilar returns true if two durations are within 20% of each other.
func durSimilar(a, b time.Duration) bool {
	if a == 0 && b == 0 {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	larger := a
	if b > larger {
		larger = b
	}
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
