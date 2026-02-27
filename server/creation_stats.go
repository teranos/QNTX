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
type CreationStatsObserver struct {
	total            atomic.Int64
	predicateContext sync.Map // map[string]*atomic.Int64 — "predicate/context" → count
}

// NewCreationStatsObserver creates a new creation stats observer.
func NewCreationStatsObserver() *CreationStatsObserver {
	return &CreationStatsObserver{}
}

// OnAttestationCreated is called asynchronously by the storage observer system.
func (o *CreationStatsObserver) OnAttestationCreated(as *types.As) {
	o.total.Add(1)

	// Track per predicate/context pair
	for _, predicate := range as.Predicates {
		for _, ctx := range as.Contexts {
			key := predicate + "/" + ctx
			val, _ := o.predicateContext.LoadOrStore(key, &atomic.Int64{})
			val.(*atomic.Int64).Add(1)
		}
	}
}

// predicateCount is a helper for sorting.
type predicateCount struct {
	Key   string
	Count int64
}

// DrainCreationCounts atomically reads and resets the accumulated creation counters.
// Returns total creations and up to 3 randomly selected top predicate/context pairs
// (sampled from top 10) formatted as "predicate/context(N)".
func (o *CreationStatsObserver) DrainCreationCounts() (total int, topPredicateContexts []string) {
	total = int(o.total.Swap(0))
	if total == 0 {
		return 0, nil
	}

	// Drain the predicate/context map
	var counts []predicateCount
	o.predicateContext.Range(func(key, value any) bool {
		count := value.(*atomic.Int64).Swap(0)
		if count > 0 {
			counts = append(counts, predicateCount{Key: key.(string), Count: count})
		}
		// Clean up zero entries
		if count == 0 {
			o.predicateContext.Delete(key)
		}
		return true
	})

	if len(counts) == 0 {
		return total, nil
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
		// 3 or fewer: show all
		topPredicateContexts = make([]string, len(top))
		for i, pc := range top {
			topPredicateContexts[i] = formatPredicateCount(pc)
		}
		return total, topPredicateContexts
	}

	// Randomly sample 3 from top 10
	rand.Shuffle(len(top), func(i, j int) {
		top[i], top[j] = top[j], top[i]
	})
	selected := top[:3]

	// Re-sort selected by count for consistent display
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Count > selected[j].Count
	})

	topPredicateContexts = make([]string, 3)
	for i, pc := range selected {
		topPredicateContexts[i] = formatPredicateCount(pc)
	}
	return total, topPredicateContexts
}

func formatPredicateCount(pc predicateCount) string {
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
