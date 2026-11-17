package server

import (
	"sync"

	"github.com/teranos/QNTX/ats/storage"
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

// 「[Opus 5.5] etcdWindows is each namespace's window. A namespace started again keeps
// one: the one before is handed back, to be unregistered and closed, as the
// watcher engine's is (startEngineIn). Two stayed registered, the second put of
// each attestation was refused, and each start left a store open (CodeRabbit on
// #1765).」
type etcdWindows struct {
	mu   sync.Mutex
	held map[string]etcdWindow
}

// 「[Opus 5.5] keep holds w for the namespace name, and hands back the one it held.」
func (e *etcdWindows) keep(name string, w etcdWindow) etcdWindow {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.held == nil {
		e.held = map[string]etcdWindow{}
	}
	before := e.held[name]
	e.held[name] = w
	return before
}
