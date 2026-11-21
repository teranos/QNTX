//go:build cgo && rustetcd

package server

import (
	"sync"

	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
)

// layesPasskeys is what laye keeps under /passkeys/ in the etcd am.toml names;
// nil when it names none.
func (s *QNTXServer) layesPasskeys() passkeysHeld {
	cfg, err := appcfg.Load()
	if err != nil || cfg.Etcd.Endpoint == "" {
		return nil
	}
	return &etcdPasskeys{endpoint: cfg.Etcd.Endpoint}
}

// etcdPasskeys opens etcd when first read, and again after any read it did
// not answer, so an etcd that came back is read whole.
type etcdPasskeys struct {
	mu       sync.Mutex
	endpoint string
	keys     *etcdcgo.Keys
}

func (e *etcdPasskeys) Held() (map[string][]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		keys, err := openEtcdKeys(e.endpoint, passkeysPrefix)
		if err != nil {
			return nil, errors.Wrapf(err, "laye's passkeys were not read from etcd at %s", e.endpoint)
		}
		e.keys = keys
	}
	held, err := e.keys.Held()
	if err != nil {
		e.keys.Close()
		e.keys = nil
		return nil, errors.Wrapf(err, "laye's passkeys were not read from etcd at %s", e.endpoint)
	}
	return held, nil
}
