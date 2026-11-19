package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats/types"
)

// 「[Opus 5.5] A namespace is put into etcd under its slug, as the registry keys it:
// "Clean" failed etcd's lowercase prefix and got no window (CodeRabbit on #1765).」
func TestTheWindowIsKeyedByTheNamespacesSlug(t *testing.T) {
	assert.Equal(t, "clean", windowNamespace("Clean"))
	assert.Equal(t, "default", windowNamespace("default"))
}

type aWindow struct {
	mu     sync.Mutex
	heard  []string
	closed bool
}

func (w *aWindow) OnAttestationCreated(as *types.As) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.heard = append(w.heard, as.ID)
}
func (w *aWindow) Close() { w.closed = true }

// 「[Opus 5.5] A namespace started again keeps one window: the one before is handed
// back to be closed (CodeRabbit on #1765).」
func TestANamespaceStartedAgainKeepsOneWindow(t *testing.T) {
	var held etcdWindows
	first, second := &aWindow{}, &aWindow{}
	_, before, _ := held.keep("garden", first)
	assert.Nil(t, before)
	_, before, _ = held.keep("garden", second)
	assert.Same(t, first, before)
	_, before, _ = held.keep("pond", &aWindow{})
	assert.Nil(t, before, "another namespace has a window of its own")
}

// 「[Opus 5.5] A namespace has one door, registered once, which hands each attestation
// to the window that is the namespace's when it arrives. Unregistering the window
// before and registering the next left a gap no write was heard in, and nothing
// said so (CodeRabbit on #1765).」
func TestANamespaceStartedAgainIsHeardThroughOneDoor(t *testing.T) {
	var held etcdWindows
	first, second := &aWindow{}, &aWindow{}
	door, _, fresh := held.keep("garden", first)
	assert.True(t, fresh, "the first window's door is new, to be registered")
	door.OnAttestationCreated(&types.As{ID: "AS-1"})

	again, _, fresh := held.keep("garden", second)
	assert.False(t, fresh, "the door stands: it is registered once")
	assert.Same(t, door, again)
	door.OnAttestationCreated(&types.As{ID: "AS-2"})

	assert.Equal(t, []string{"AS-1"}, first.heard)
	assert.Equal(t, []string{"AS-2"}, second.heard, "what came after the start went to the window that is the namespace's now")
}
