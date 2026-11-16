//go:build cgo && rustetcd

package server

import (
	"sync"

	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] collectedKeysAt is where what is collected is kept in etcd, a key
// 「[Sonnet 5.5] for each agent.」」
const collectedKeysAt = "/collected/"

// 「[Opus 5.5] collectedKeysOfNode is the etcd am.toml names, as the keys what is
// 「[Opus 5.5] collected is kept at; nil when it names none.」」
func (s *QNTXServer) collectedKeysOfNode() collectedKeeper {
	cfg, err := appcfg.Load()
	if err != nil || cfg.Etcd.Endpoint == "" {
		// 「[Opus 5.5] am.toml that does not read refuses the node elsewhere first (the etcd window).」
		return nil
	}
	return &etcdCollectedKeys{endpoint: cfg.Etcd.Endpoint}
}

// 「[Opus 5.5] etcdCollectedKeys opens etcd when first set, and again after any
// 「[Opus 5.5] set it did not take, so an etcd that came back is written to whole.」」
type etcdCollectedKeys struct {
	mu       sync.Mutex
	endpoint string
	keys     *etcdcgo.Keys
}

func (e *etcdCollectedKeys) Set(whole map[string][]byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		keys, err := etcdcgo.OpenKeys(e.endpoint, collectedKeysAt)
		if err != nil {
			return errors.Wrapf(err, "what is collected was not kept in etcd at %s", e.endpoint)
		}
		e.keys = keys
	}
	if err := e.keys.Set(whole); err != nil {
		e.keys.Close()
		e.keys = nil
		return errors.Wrapf(err, "what is collected was not kept in etcd at %s", e.endpoint)
	}
	return nil
}
