package server

import (
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/teranos/QNTX/ats/types"
)

// CreationStatsObserver implements storage.AttestationObserver and accumulates
// creation counts for periodic summary logging by the Pulse ticker.
// Tracks three dimensions that cycle each drain:
//   - "predicate of context" (e.g. "posted of atproto")
//   - "subject predicate" (e.g. "did:plc:xxx posted")
//   - "context by actor" (e.g. "atproto by glyph:abc")
type CreationStatsObserver struct {
	total         atomic.Int64
	predContext   sync.Map // "predicate of context" → *atomic.Int64
	subjectPred   sync.Map // "subject predicate" → *atomic.Int64
	contextActor  sync.Map // "context by actor" → *atomic.Int64
	drainCycle    atomic.Int64
}

// NewCreationStatsObserver creates a new creation stats observer.
func NewCreationStatsObserver() *CreationStatsObserver {
	return &CreationStatsObserver{}
}

// OnAttestationCreated is called asynchronously by the storage observer system.
func (o *CreationStatsObserver) OnAttestationCreated(as *types.As) {
	o.total.Add(1)

	for _, predicate := range as.Predicates {
		for _, ctx := range as.Contexts {
			key := predicate + " of " + ctx
			val, _ := o.predContext.LoadOrStore(key, &atomic.Int64{})
			val.(*atomic.Int64).Add(1)
		}
		for _, subject := range as.Subjects {
			key := subject + " " + predicate
			val, _ := o.subjectPred.LoadOrStore(key, &atomic.Int64{})
			val.(*atomic.Int64).Add(1)
		}
	}

	for _, ctx := range as.Contexts {
		for _, actor := range as.Actors {
			key := ctx + " by " + actor
			val, _ := o.contextActor.LoadOrStore(key, &atomic.Int64{})
			val.(*atomic.Int64).Add(1)
		}
	}
}

// pairCount is a helper for sorting.
type pairCount struct {
	Key   string
	Count int64
}

// DrainCreationCounts atomically reads and resets the accumulated creation counters.
// Cycles through three views: predicate of context → subject predicate → context by actor.
// Returns total creations and up to 3 randomly selected top pairs (sampled from top 10).
func (o *CreationStatsObserver) DrainCreationCounts() (total int, topPairs []string) {
	total = int(o.total.Swap(0))
	if total == 0 {
		return 0, nil
	}

	maps := []*sync.Map{&o.predContext, &o.subjectPred, &o.contextActor}
	active := int(o.drainCycle.Add(1) % 3)

	// Drain all maps, only format the active one
	for i, m := range maps {
		if i == active {
			topPairs = drainAndSample(m)
		} else {
			drainMap(m)
		}
	}

	return total, topPairs
}

// drainMap resets all counters in a sync.Map without collecting results.
func drainMap(m *sync.Map) {
	m.Range(func(key, value any) bool {
		count := value.(*atomic.Int64).Swap(0)
		if count == 0 {
			m.Delete(key)
		}
		return true
	})
}

// drainAndSample drains a sync.Map and returns up to 3 randomly sampled top pairs.
func drainAndSample(m *sync.Map) []string {
	var counts []pairCount
	m.Range(func(key, value any) bool {
		count := value.(*atomic.Int64).Swap(0)
		if count > 0 {
			counts = append(counts, pairCount{Key: key.(string), Count: count})
		}
		if count == 0 {
			m.Delete(key)
		}
		return true
	})

	if len(counts) == 0 {
		return nil
	}

	// Sort by count descending
	sort.Slice(counts, func(i, j int) bool {
		return counts[i].Count > counts[j].Count
	})

	// Take top 10, then randomly pick 3
	top := counts
	if len(top) > 10 {
		top = top[:10]
	}

	if len(top) <= 3 {
		result := make([]string, len(top))
		for i, pc := range top {
			result[i] = formatPairCount(pc)
		}
		return result
	}

	rand.Shuffle(len(top), func(i, j int) {
		top[i], top[j] = top[j], top[i]
	})
	selected := top[:3]

	// Re-sort selected by count for consistent display
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Count > selected[j].Count
	})

	result := make([]string, 3)
	for i, pc := range selected {
		result[i] = formatPairCount(pc)
	}
	return result
}

func formatPairCount(pc pairCount) string {
	return pc.Key + "(" + itoa(pc.Count) + ")"
}

// itoa converts int64 to string without importing strconv.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
