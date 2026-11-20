package server

import (
	"sync"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/slug"
)

// 「[Opus 5.5] windowNamespace is the name a namespace is put into etcd under: its
// slug, as the registry keys it. etcd's prefix takes lowercase alone, and "Clean"
// got no window (CodeRabbit on #1765).」
func windowNamespace(name string) string { return slug.Of(name) }

// 「[Opus 5.5] etcdWindow is a namespace's window, as the node keeps it.」
type etcdWindow interface {
	storage.AttestationObserver
	Close()
}

// 「[Opus 5.5] windowDoor is what a namespace registers, once: it hands each attestation
// to the window that is the namespace's when the attestation arrives. Unregistering
// the window before and registering the next left a gap no write was heard in,
// and nothing said so (CodeRabbit on #1765). A write the door handed to the window
// before is put by it, or handed to failed as that window closes.」
type windowDoor struct {
	mu     sync.RWMutex
	window etcdWindow
}

func (d *windowDoor) OnAttestationCreated(as *types.As) {
	d.mu.RLock()
	w := d.window
	d.mu.RUnlock()
	w.OnAttestationCreated(as)
}

func (d *windowDoor) Close() {}

// 「[Opus 5.5] etcdWindows is each namespace's door and the window behind it. A namespace
// started again keeps one window: the one before is handed back to be closed, as
// the watcher engine's is (startEngineIn). Two stayed registered, the second put of
// each attestation was refused, and each start left a store open (CodeRabbit on
// #1765).」
type etcdWindows struct {
	mu    sync.Mutex
	doors map[string]*windowDoor
}

// 「[Opus 5.5] keep puts w behind the namespace's door. It answers the door, the window
// it held before, and whether the door is new and is to be registered.」
func (e *etcdWindows) keep(name string, w etcdWindow) (door etcdWindow, before etcdWindow, fresh bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.doors == nil {
		e.doors = map[string]*windowDoor{}
	}
	d, held := e.doors[name]
	if !held {
		d = &windowDoor{window: w}
		e.doors[name] = d
		return d, nil, true
	}
	d.mu.Lock()
	before = d.window
	d.window = w
	d.mu.Unlock()
	return d, before, false
}
