package server

import (
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

type aWindow struct{ closed bool }

func (*aWindow) OnAttestationCreated(*types.As) {}
func (w *aWindow) Close()                       { w.closed = true }

// 「[Opus 5.5] A namespace started again keeps one window: the one before is handed
// back, to be unregistered and closed, as the watcher engine's is (CodeRabbit on #1765).」
func TestANamespaceStartedAgainKeepsOneWindow(t *testing.T) {
	var held etcdWindows
	first, second := &aWindow{}, &aWindow{}
	assert.Nil(t, held.keep("garden", first))
	assert.Same(t, first, held.keep("garden", second))
	assert.Nil(t, held.keep("pond", &aWindow{}), "another namespace has a window of its own")
}
