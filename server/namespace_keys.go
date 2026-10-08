package server

// A namespace's keys (ADR-051). A key is a line in the namespace, its value
// sealed under a key derived from the node's own for that namespace. Newest
// line per name holds, and a dropped key is a line that says so.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// A KEY line's predicate is the key's name.
const keySubject = "KEY"

const keysPath = "/api/keys"

// keyNameMax is the longest a key's name may be.
const keyNameMax = 64

// sealingKey is the 32 bytes a namespace's keys are sealed under: the HMAC of
// the namespace's name under the node key's own seed, the way a derived token's
// seed is (internal/access).
func sealingKey(node ed25519.PrivateKey, namespace string) ([]byte, error) {
	if len(node) != ed25519.PrivateKeySize {
		return nil, errors.Newf("the keys of %s are sealed under a key derived from an ed25519 key, and this one is %d bytes", namespace, len(node))
	}
	mac := hmac.New(sha256.New, node.Seed())
	if _, err := mac.Write([]byte("qntx:namespace-keys:" + namespace)); err != nil {
		return nil, errors.Wrapf(err, "the sealing key of %s was not derived", namespace)
	}
	return mac.Sum(nil), nil
}

// sealedFor is what a sealed value is bound to: its namespace and its name, so
// a blob moved to another of either does not unseal.
func sealedFor(namespace, name string) []byte {
	return []byte(namespace + "\x00" + name)
}

func keyCipher(node ed25519.PrivateKey, namespace string) (cipher.AEAD, error) {
	key, err := sealingKey(node, namespace)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrapf(err, "the cipher for the keys of %s was not made", namespace)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrapf(err, "the cipher for the keys of %s was not made", namespace)
	}
	return gcm, nil
}

// sealKey seals one value: a fresh nonce, then the ciphertext, as base64.
func sealKey(node ed25519.PrivateKey, namespace, name string, value []byte) (string, error) {
	gcm, err := keyCipher(node, namespace)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.Wrapf(err, "no nonce to seal %s of %s with", name, namespace)
	}
	sealed := gcm.Seal(nonce, nonce, value, sealedFor(namespace, name))
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// unsealKey opens what sealKey sealed for the same namespace and name.
func unsealKey(node ed25519.PrivateKey, namespace, name, sealed string) ([]byte, error) {
	gcm, err := keyCipher(node, namespace)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, errors.Wrapf(err, "the sealed %s of %s is not base64", name, namespace)
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.Newf("the sealed %s of %s is %d bytes, shorter than its nonce", name, namespace, len(raw))
	}
	value, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], sealedFor(namespace, name))
	if err != nil {
		return nil, errors.Wrapf(err, "the sealed %s of %s did not unseal", name, namespace)
	}
	return value, nil
}

// keyNameRefused is why a name is not a key's name, or empty when it is one:
// letters, digits, dash, underscore and dot, one to keyNameMax of them.
func keyNameRefused(name string) string {
	if name == "" || len(name) > keyNameMax {
		return "a key's name is 1 to 64 letters, digits, dashes, underscores and dots, and this is " + name
	}
	for _, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		if !letter && !digit && !strings.ContainsRune("-_.", r) {
			return "a key's name is 1 to 64 letters, digits, dashes, underscores and dots, and this is " + name
		}
	}
	return ""
}

// newestKeys is the newest KEY line per name in a namespace's store, dropped
// ones included.
func newestKeys(store ats.AttestationStore, namespace string) (map[string]*types.As, error) {
	found, err := store.GetAttestations(ats.AttestationFilter{
		Subjects: []string{keySubject},
		Limit:    storage.MaxAttestationLimit,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines in %s", keySubject, namespace)
	}
	newest := map[string]*types.As{}
	for _, as := range found {
		if len(as.Subjects) != 1 || as.Subjects[0] != keySubject || len(as.Predicates) != 1 {
			continue
		}
		name := as.Predicates[0]
		if held, ok := newest[name]; ok && !as.Timestamp.After(held.Timestamp) {
			continue
		}
		newest[name] = as
	}
	return newest, nil
}

func keyDropped(as *types.As) bool {
	dropped, _ := as.Attributes["dropped"].(bool)
	return dropped
}

// writeKey writes one KEY line about name.
func writeKey(store ats.AttestationStore, namespace, owner, name string, attributes map[string]any, at time.Time) error {
	id, err := identity.GenerateASUIDWithRetry("AS", keySubject, name, "_", store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line about %s in %s", keySubject, name, namespace)
	}
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{keySubject}, Predicates: []string{name}, Contexts: []string{"_"},
		Actors: []string{owner}, Timestamp: at, CreatedAt: at, Source: pluginSource, Attributes: attributes,
	}); err != nil {
		return errors.Wrapf(err, "the %s line about %s in %s was not written", keySubject, name, namespace)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

// unsealedKeys is every live key of a namespace, by name, unsealed.
func unsealedKeys(store ats.AttestationStore, namespace string, node ed25519.PrivateKey) (map[string]string, error) {
	newest, err := newestKeys(store, namespace)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, as := range newest {
		if keyDropped(as) {
			continue
		}
		sealed, ok := as.Attributes["sealed"].(string)
		if !ok {
			return nil, errors.Newf("%s line %s about %s in %s holds no sealed value", keySubject, as.ID, name, namespace)
		}
		value, err := unsealKey(node, namespace, name, sealed)
		if err != nil {
			return nil, err
		}
		out[name] = string(value)
	}
	return out, nil
}

// keyListed is one live key as the sigils give it. Never a value.
type keyListed struct {
	Name  string `json:"name"`
	SetBy string `json:"set_by"`
	SetAt string `json:"set_at"`
}

func listedKeys(store ats.AttestationStore, namespace string) ([]keyListed, error) {
	newest, err := newestKeys(store, namespace)
	if err != nil {
		return nil, err
	}
	keys := []keyListed{}
	for name, as := range newest {
		if keyDropped(as) {
			continue
		}
		setBy, _ := as.Attributes["set_by"].(string)
		keys = append(keys, keyListed{Name: name, SetBy: setBy, SetAt: as.Timestamp.UTC().Format(time.RFC3339)})
	}
	slices.SortFunc(keys, func(a, b keyListed) int { return strings.Compare(a.Name, b.Name) })
	return keys, nil
}

func (s *QNTXServer) keysSignum() sigil.Signum {
	listed := []*protocol.Field{{Name: "keys", Says: "One per key the namespace holds: its name, who set it (set_by) and when (set_at, RFC 3339). Never a value."}}
	name := &protocol.Param{Name: "name", Required: true, Says: "The key's name: 1 to 64 letters, digits, dashes, underscores and dots."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "keys",
			Sigils: []*protocol.Sigil{
				{
					Name:  "list",
					Does:  "Every key the namespace the caller stands in holds, by name. No value is ever answered. Refused to a token and a connector; set and listed by ROOT, SUPER and the namespace's owner.",
					Gives: listed,
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: keysPath},
				},
				{
					Name: "set",
					Does: "Set a key of the namespace the caller stands in to a value, sealed by the node. Refused to a token and a connector; set by ROOT, SUPER and the namespace's owner.",
					Takes: []*protocol.Param{
						name,
						{Name: "value", Required: true, Says: "The key's value. Kept sealed and never answered."},
					},
					Gives: listed,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: keysPath},
				},
				{
					Name:  "drop",
					Does:  "Drop a key of the namespace the caller stands in. Refused to a token and a connector; dropped by ROOT, SUPER and the namespace's owner.",
					Takes: []*protocol.Param{name},
					Gives: listed,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: keysPath + "/drop"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"list": s.keysList, "set": s.keysSet, "drop": s.keysDrop},
	}
}

// keysMay is who reaches a namespace's keys, and the namespace: never a token
// or a connector, then ROOT and SUPER, then the owner its ns.toml names.
func (s *QNTXServer) keysMay(ctx context.Context) (string, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return "", &protocol.Refusal{Why: sigil.NotAllowed, Says: "a namespace's keys are asked through the gate, and this was asked outside it, standing in no namespace"}
	}
	namespace := s.namespaceOf(admitted)
	if admitted.Grant != nil || admitted.ClientDID != "" {
		return "", &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "a key is set and listed by a person and never by a token, and this asked of the keys of " + namespace + " as one"}
	}
	if admitted.CrossesNamespaces() {
		return namespace, nil
	}
	ownedOnly := &protocol.Refusal{Why: sigil.NotAllowed,
		Says: "the keys of " + namespace + " are set by ROOT, SUPER and its owner, and " + namespace + " has no ns.toml naming an owner"}
	known := s.held.Known()
	if known == nil {
		return "", ownedOnly
	}
	listed, err := known.List()
	if err != nil {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "cannot tell who owns %s", namespace).Error()}
	}
	found, err := namespaces.Named(listed, namespace)
	var notServed namespaces.NotServed
	if errors.As(err, &notServed) {
		return "", ownedOnly
	}
	if err != nil {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "cannot tell who owns %s", namespace).Error()}
	}
	if found.Definition == nil || found.Definition.Owner == "" {
		return "", ownedOnly
	}
	if asked := askedBy(ctx); asked == "" || asked != found.Definition.Owner {
		return "", &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "the keys of " + namespace + " are set by ROOT, SUPER and its owner " + found.Definition.Owner + ", and this was asked by " + asked}
	}
	return namespace, nil
}

// keysStore is the store of the namespace the caller stands in, as am ground
// reaches it.
func (s *QNTXServer) keysStore(ctx context.Context, namespace string) (ats.AttestationStore, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the keys of " + namespace + " are reached through a request, and this carried none"}
	}
	store, err := s.storeFor(caller)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "no store to keep the keys of %s in", namespace).Error()}
	}
	return store, nil
}

func (s *QNTXServer) keysAnswer(store ats.AttestationStore, namespace string) (any, *protocol.Refusal) {
	keys, err := listedKeys(store, namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{"keys": keys}, nil
}

func (s *QNTXServer) keysList(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	namespace, refused := s.keysMay(ctx)
	if refused != nil {
		return nil, refused
	}
	store, refused := s.keysStore(ctx, namespace)
	if refused != nil {
		return nil, refused
	}
	return s.keysAnswer(store, namespace)
}

func (s *QNTXServer) keysSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	namespace, refused := s.keysMay(ctx)
	if refused != nil {
		return nil, refused
	}
	name := sent["name"]
	if why := keyNameRefused(name); why != "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: why}
	}
	value := sent["value"]
	if value == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "value", Says: "a key of " + namespace + " is set to a value, and " + name + " was sent none"}
	}
	if s.nodeDID == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no key of its own to seal " + name + " of " + namespace + " under"}
	}
	store, refused := s.keysStore(ctx, namespace)
	if refused != nil {
		return nil, refused
	}
	sealed, err := sealKey(s.nodeDID.PrivateKey, namespace, name, []byte(value))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	owner := askedBy(ctx)
	if err := writeKey(store, namespace, owner, name, map[string]any{"sealed": sealed, "set_by": owner}, time.Now()); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.keysAnswer(store, namespace)
}

func (s *QNTXServer) keysDrop(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	namespace, refused := s.keysMay(ctx)
	if refused != nil {
		return nil, refused
	}
	name := sent["name"]
	if why := keyNameRefused(name); why != "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: why}
	}
	store, refused := s.keysStore(ctx, namespace)
	if refused != nil {
		return nil, refused
	}
	owner := askedBy(ctx)
	if err := writeKey(store, namespace, owner, name, map[string]any{"dropped": true, "set_by": owner}, time.Now()); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.keysAnswer(store, namespace)
}
