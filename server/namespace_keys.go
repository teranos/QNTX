package server

// A namespace's keys (ADR-051).
//
// "customer, or friend who helps ne test in this casem provide their own key"
//
// "User set's up other things that matter to them like sentry or they set the
// openrouter or anthropic api key."
//
// A key is a line in the namespace: subject KEY, predicate the key's name,
// actor who set it, the value sealed in an attribute under a key derived from
// the node's own for that namespace. No sigil answers a value: keys list
// names, keys set sets one, keys drop drops one. A value leaves the node once,
// into the environment of the harness process the node starts for that
// namespace.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/admission"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

const keyLineSubject = "KEY"

// A key line holds its value sealed, or says the key was dropped.
const (
	keySealed  = "sealed"
	keyDropped = "dropped"
)

// keysSignum is a namespace's keys, at the namespace's path.
func (s *QNTXServer) keysSignum() sigil.Signum {
	at := "/api/keys/{namespace}"
	namespace := &protocol.Param{Name: "namespace", Required: true, Says: "The namespace whose keys these are."}
	name := &protocol.Param{Name: "name", Required: true, Says: "The key's name, e.g. openrouter."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "keys",
			Description: "A namespace's keys: set by its owner, ROOT or SUPER, handed to the harness the node starts for the namespace, and never answered back.",
			Tags:        []string{"namespaces", "keys"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "The names of the namespace's keys, who set each and when. A value is never answered.",
					Takes:  []*protocol.Param{namespace},
					Answer: "protocol.NamespaceKeys",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				{
					Name:   "set",
					Does:   "Sets a key of the namespace to a value, sealed. Set by the namespace's owner, ROOT or SUPER, never by a token or a connector.",
					Takes:  []*protocol.Param{namespace, name, {Name: "value", Required: true, Says: "The key's value. It is sealed, and never answered back."}},
					Answer: "protocol.NamespaceKeys",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
				{
					Name:   "drop",
					Does:   "Drops a key of the namespace. Dropped by the namespace's owner, ROOT or SUPER, never by a token or a connector.",
					Takes:  []*protocol.Param{namespace, name},
					Answer: "protocol.NamespaceKeys",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at + "/drop"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list": s.keysList,
			"set":  s.keysSet,
			"drop": s.keysDrop,
		},
	}
}

// keysAsking is the namespace a call on the keys signum names, when a person
// asks it: "a model never lists, sets or reads a key", so a token and a
// connector are refused at any level. Listing is the owner's, ROOT's and
// SUPER's, as setting is.
func (s *QNTXServer) keysAsking(ctx context.Context, sent sigil.Sent) (anAsking, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	return then(asked, refused, func(asked anAsking) (anAsking, *protocol.Refusal) {
		switch asked.admitted.Asker().(type) {
		case admission.TokenAsked:
			return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "a namespace's keys are a person's to handle, and a token asked"}
		}
		if client, err := named.TextOf("the connector's client", asked.admitted.ClientDID); err == nil {
			return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "a namespace's keys are a person's to handle, and the connector " + client.String() + " asked"}
		}
		return asked, s.mayHandle(asked.admitted, asked.namespace, "the keys of "+asked.namespace+" are handled")
	})
}

func (s *QNTXServer) keysList(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.keysAsking(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		return s.keysAskedOf(ctx, asked.namespace)
	})
}

// keysAskedOf is the keys the namespace holds, each named after a provider
// asked of that provider.
func (s *QNTXServer) keysAskedOf(ctx context.Context, namespace string) (*protocol.NamespaceKeys, *protocol.Refusal) {
	kept, refused := s.keysOf(namespace)
	if refused == nil {
		s.askProviders(ctx, kept)
	}
	return kept, refused
}

func (s *QNTXServer) keysSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.keysAsking(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		value, err := named.TextOf("value", sent["value"])
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "value", Says: err.Error() + ": a key is set to a value, and dropped with keys drop"}
		}
		sealed, err := s.sealKey(asked.namespace, value.String())
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		if err := s.setKeyLine(asked.admitted, asked.namespace, name.String(), map[string]any{keySealed: sealed}); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		s.logger.Infow("a namespace's key was set", "namespace", asked.namespace, "key", name.String(), "by", asked.admitted.Identity)
		return s.keysAskedOf(ctx, asked.namespace)
	})
}

func (s *QNTXServer) keysDrop(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.keysAsking(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		if err := s.setKeyLine(asked.admitted, asked.namespace, name.String(), map[string]any{keyDropped: true}); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		s.logger.Infow("a namespace's key was dropped", "namespace", asked.namespace, "key", name.String(), "by", asked.admitted.Identity)
		return s.keysOf(asked.namespace)
	})
}

// setKeyLine writes one key line of the namespace, as whoever set it.
func (s *QNTXServer) setKeyLine(admitted auth.Admission, namespace, name string, attributes map[string]any) error {
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to write the %s line %s of %s in", keyLineSubject, name, namespace)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", keyLineSubject, name, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line %s of %s", keyLineSubject, name, namespace)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{keyLineSubject}, Predicates: []string{name}, Contexts: []string{namespace},
		Actors: []string{admitted.Identity}, Timestamp: at, CreatedAt: at, Source: "keys",
		Attributes: attributes,
	}), "the %s line %s of %s was not written", keyLineSubject, name, namespace)
}

// newestKeys is the newest key line of the namespace for each name.
func (s *QNTXServer) newestKeys(namespace string) (map[string]*types.As, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines of %s", keyLineSubject, namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{keyLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines of %s", keyLineSubject, namespace)
	}
	newest := map[string]*types.As{}
	for _, as := range found {
		for _, name := range as.Predicates {
			if held, ok := newest[name]; !ok || as.Timestamp.After(held.Timestamp) {
				newest[name] = as
			}
		}
	}
	return newest, nil
}

// keysOf is the names of the keys the namespace holds: each one whose newest
// line holds a value.
func (s *QNTXServer) keysOf(namespace string) (*protocol.NamespaceKeys, *protocol.Refusal) {
	newest, err := s.newestKeys(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept := &protocol.NamespaceKeys{Namespace: namespace, Keys: []*protocol.NamespaceKey{}}
	for name, as := range newest {
		switch as.Attributes[keySealed].(type) {
		case string:
			setters, err := named.ItemsOf(keyLineSubject+" line "+as.ID+" of "+namespace+", who set it,", as.Actors)
			if err != nil {
				return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
			}
			kept.Keys = append(kept.Keys, &protocol.NamespaceKey{Name: name, SetBy: setters.First(), At: as.Timestamp.UTC().Format(time.RFC3339)})
		}
	}
	sort.Slice(kept.Keys, func(i, j int) bool { return kept.Keys[i].Name < kept.Keys[j].Name })
	return kept, nil
}

// keyValue is the value of the namespace's key of that name, unsealed, for
// the harness process the node starts for the namespace and nothing else.
func (s *QNTXServer) keyValue(namespace, name string) (string, error) {
	newest, err := s.newestKeys(namespace)
	if err != nil {
		return "", err
	}
	as, set := newest[name]
	if !set {
		return "", errors.Newf("%s holds no key %s: set one with keys set", namespace, name)
	}
	sealed, holds := as.Attributes[keySealed].(string)
	if !holds {
		return "", errors.Newf("the key %s of %s was dropped: set it again with keys set", name, namespace)
	}
	return s.unsealKey(namespace, sealed)
}

// sealerOf is the AEAD what is sealed for the purpose in a namespace is sealed
// with: AES-256-GCM under a key derived from the node's own for both.
func (s *QNTXServer) sealerOf(purpose, namespace string) (cipher.AEAD, error) {
	node, err := named.Pointed("this node's key, which the "+purpose+" of "+namespace+" are sealed under,", s.nodeDID)
	if err != nil {
		return nil, err
	}
	derived, err := access.DeriveKey(node.PrivateKey, purpose+":"+slug.Of(namespace))
	if err != nil {
		return nil, errors.Wrapf(err, "no key to seal the %s of %s under", purpose, namespace)
	}
	block, err := aes.NewCipher(derived.Seed())
	if err != nil {
		return nil, errors.Wrapf(err, "the %s of %s have no cipher", purpose, namespace)
	}
	return cipher.NewGCM(block)
}

// sealKey is value sealed for the namespace: the nonce, then what was sealed.
func (s *QNTXServer) sealKey(namespace, value string) (string, error) {
	return s.sealFor("keys", namespace, value)
}

// unsealKey is what sealKey sealed for the namespace.
func (s *QNTXServer) unsealKey(namespace, sealed string) (string, error) {
	return s.unsealFor("keys", namespace, sealed)
}

// sealFor is value sealed for the purpose in the namespace: the nonce, then
// what was sealed.
func (s *QNTXServer) sealFor(purpose, namespace, value string) (string, error) {
	aead, err := s.sealerOf(purpose, namespace)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	read, err := rand.Read(nonce)
	if err != nil {
		return "", errors.Wrapf(err, "no nonce to seal the %s of %s with", purpose, namespace)
	}
	if read != len(nonce) {
		return "", errors.Newf("the nonce to seal the %s of %s with is %d bytes of %d", purpose, namespace, read, len(nonce))
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(value), []byte(namespace))), nil
}

// unsealFor is what sealFor sealed for the purpose in the namespace.
func (s *QNTXServer) unsealFor(purpose, namespace, sealed string) (string, error) {
	aead, err := s.sealerOf(purpose, namespace)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", errors.Wrapf(err, "the %s of %s are not sealed as the node seals them", purpose, namespace)
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.Newf("what was sealed of the %s of %s is %d bytes, shorter than its nonce", purpose, namespace, len(raw))
	}
	opened, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(namespace))
	if err != nil {
		return "", errors.Wrapf(err, "what was sealed of the %s of %s does not open under the node's key for them", purpose, namespace)
	}
	return string(opened), nil
}
