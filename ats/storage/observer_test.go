package storage

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/types"
)

// recorder is an observer that remembers what it was handed.
type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) OnAttestationCreated(as *types.As) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, as.ID)
}

func (r *recorder) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// TestNotifyObserversStaysInItsNamespace is the whole point of the registry:
// "A watcher in namespace A does not fire on an attestation in namespace B.
// They are not the same world." (ADR-026)
func TestNotifyObserversStaysInItsNamespace(t *testing.T) {
	ClearObservers()
	t.Cleanup(ClearObservers)

	inA := &recorder{}
	inB := &recorder{}
	RegisterObserver("a", inA)
	RegisterObserver("b", inB)

	NotifyObserversSync("a", &types.As{ID: "AS-in-a"})
	NotifyObserversSync("b", &types.As{ID: "AS-in-b"})

	assert.Equal(t, []string{"AS-in-a"}, inA.ids(), "the observer of a saw something written in b")
	assert.Equal(t, []string{"AS-in-b"}, inB.ids(), "the observer of b saw something written in a")
}

// TestNotifyObserversUnwatchedNamespaceIsSilent covers the store that names no
// namespace: it reaches nobody rather than reaching everybody.
func TestNotifyObserversUnwatchedNamespaceIsSilent(t *testing.T) {
	ClearObservers()
	t.Cleanup(ClearObservers)

	watching := &recorder{}
	RegisterObserver("a", watching)

	NotifyObserversSync("", &types.As{ID: "AS-nowhere"})
	NotifyObserversSync("c", &types.As{ID: "AS-in-c"})

	assert.Empty(t, watching.ids(), "an unwatched namespace reached an observer of another")
}

// TestUnregisterObserverLeavesOtherNamespaces: an observer registered for two
// universes and removed from one keeps the other.
func TestUnregisterObserverLeavesOtherNamespaces(t *testing.T) {
	ClearObservers()
	t.Cleanup(ClearObservers)

	both := &recorder{}
	RegisterObserver("a", both)
	RegisterObserver("b", both)

	UnregisterObserver("a", both)

	NotifyObserversSync("a", &types.As{ID: "AS-in-a"})
	NotifyObserversSync("b", &types.As{ID: "AS-in-b"})

	assert.Equal(t, []string{"AS-in-b"}, both.ids())
}

// TestAtsStoreNotifiesItsOwnNamespace is the seam the store side has to hold:
// the namespace an AtsStore was opened for is the one its writes notify.
func TestAtsStoreNotifiesItsOwnNamespace(t *testing.T) {
	ClearObservers()
	t.Cleanup(ClearObservers)

	inA := &recorder{}
	inB := &recorder{}
	RegisterObserver("a", inA)
	RegisterObserver("b", inB)

	storeB := NewAtsStore(&stubRaw{}, nil, "b")
	require.NoError(t, storeB.CreateAttestation(&types.As{ID: "AS-written-in-b"}))

	// A write notifies asynchronously, so the observer that should see it is
	// waited for and the one that should not is checked after that wait: by
	// then it has had at least as long to be wrongly notified.
	require.Eventually(t, func() bool {
		return len(inB.ids()) == 1
	}, 2*time.Second, 5*time.Millisecond, "the observer of b never saw a write in b")

	assert.Equal(t, []string{"AS-written-in-b"}, inB.ids())
	assert.Empty(t, inA.ids(), "a write in b notified the observer of a")
}
