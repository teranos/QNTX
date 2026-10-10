package server

// A plugin stands in the namespace its record names (ADR-046). The node hands
// it a token of its own at Initialize, and that token reaches that namespace's
// store at the ATS store and fetch services, as a call's token reaches the caller's.

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/errors"
)

// standingPlugin is what a plugin's token holds: the plugin, the namespace its
// record names, and that namespace's store.
type standingPlugin struct {
	plugin    string
	namespace string
	store     ats.AttestationStore
}

// pluginToken mints a plugin the token that reaches the namespace its record
// names, replacing the one it held. Minted at each Initialize, so a record
// that moved a plugin moves where it reads and writes.
func (s *QNTXServer) pluginToken(plugin, namespace string) (string, error) {
	store, err := s.held.OfPlugin(namespace)
	if err != nil {
		return "", errors.Wrapf(err, "namespace %s is not served", namespace)
	}
	if store == nil {
		return "", errors.Newf("namespace %s holds no store", namespace)
	}
	raw := make([]byte, 32)
	if drawn, err := rand.Read(raw); err != nil {
		return "", errors.Wrapf(err, "no token could be drawn for plugin %s: %d of %d bytes drawn", plugin, drawn, len(raw))
	}
	token := hex.EncodeToString(raw)
	if before, held := s.pluginTokens.Load(plugin); held {
		s.pluginStores.Delete(before.(string))
	}
	s.pluginStores.Store(token, standingPlugin{plugin: plugin, namespace: namespace, store: store})
	s.pluginTokens.Store(plugin, token)
	return token, nil
}

// storeOfPlugin is the store a plugin's own token reaches.
func (s *QNTXServer) storeOfPlugin(token string) (ats.AttestationStore, bool) {
	held, standing := s.pluginStores.Load(token)
	if !standing {
		return nil, false
	}
	p, ok := held.(standingPlugin)
	return p.store, ok
}
