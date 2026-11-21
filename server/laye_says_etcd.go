//go:build cgo && rustetcd

package server

import (
	"encoding/json"
	"sync"

	"github.com/teranos/QNTX/ats/storage/etcdcgo"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "AND LAYE OWNS IDENTITY"
//
// layeSays is laye's record of whose a key is, in the etcd am.toml names: a
// key a person said yes to (/keys/<did>), and a session's key one of those
// admitted (/admitted/<did>). Nil when am.toml names no etcd.
func layeSays() auth.LayeSays {
	cfg, err := appcfg.Load()
	if err != nil || cfg.Etcd.Endpoint == "" {
		return nil
	}
	keys := &etcdPrefix{endpoint: cfg.Etcd.Endpoint, prefix: "/keys/"}
	admitted := &etcdPrefix{endpoint: cfg.Etcd.Endpoint, prefix: "/admitted/"}
	return func(did string) (string, bool, error) {
		if person, found, err := personOf(keys, did); err != nil || found {
			return person, found, err
		}
		held, err := admitted.held()
		if err != nil {
			return "", false, err
		}
		raw, found := held[did]
		if !found {
			return "", false, nil
		}
		var admission struct {
			By string `json:"by"`
		}
		if err := json.Unmarshal(raw, &admission); err != nil || admission.By == "" {
			return "", false, errors.Newf("laye's admission of %s names no key that admitted it", did)
		}
		return personOf(keys, admission.By)
	}
}

// personOf is the person laye's /keys/ record names for did.
func personOf(keys *etcdPrefix, did string) (string, bool, error) {
	held, err := keys.held()
	if err != nil {
		return "", false, err
	}
	raw, found := held[did]
	if !found {
		return "", false, nil
	}
	var yes struct {
		Person string `json:"person"`
	}
	if err := json.Unmarshal(raw, &yes); err != nil || yes.Person == "" {
		return "", false, errors.Newf("laye's record of %s names no person", did)
	}
	return yes.Person, true, nil
}

// etcdPrefix opens etcd when first read, and again after a read it did not answer.
type etcdPrefix struct {
	mu       sync.Mutex
	endpoint string
	prefix   string
	keys     *etcdcgo.Keys
}

func (e *etcdPrefix) held() (map[string][]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		keys, err := etcdcgo.OpenKeys(e.endpoint, e.prefix)
		if err != nil {
			return nil, errors.Wrapf(err, "laye's %s was not read from etcd at %s", e.prefix, e.endpoint)
		}
		e.keys = keys
	}
	held, err := e.keys.Held()
	if err != nil {
		e.keys.Close()
		e.keys = nil
		return nil, errors.Wrapf(err, "laye's %s was not read from etcd at %s", e.prefix, e.endpoint)
	}
	return held, nil
}
