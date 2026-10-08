package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
)

// "Handing the node their key: no place exists."

func nodeKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return key
}

func TestAKeySealedUnsealsByteEqual(t *testing.T) {
	node := nodeKey(t)
	value := []byte("sk-or-v1-abc\nsecond line\nkëy ✻ 鍵\n")
	sealed, err := sealKey(node, "garden", "OPENROUTER_API_KEY", value)
	require.NoError(t, err)
	assert.NotContains(t, sealed, "sk-or")

	opened, err := unsealKey(node, "garden", "OPENROUTER_API_KEY", sealed)
	require.NoError(t, err)
	assert.Equal(t, value, opened)

	again, err := sealKey(node, "garden", "OPENROUTER_API_KEY", value)
	require.NoError(t, err)
	assert.NotEqual(t, sealed, again, "each seal has its own nonce")
}

func TestASealedKeyMovedDoesNotUnseal(t *testing.T) {
	node := nodeKey(t)
	sealed, err := sealKey(node, "garden", "SENTRY_DSN", []byte("https://x@sentry.example/1"))
	require.NoError(t, err)

	_, err = unsealKey(node, "garden", "ANTHROPIC_API_KEY", sealed)
	assert.Error(t, err, "under another name")
	_, err = unsealKey(node, "orchard", "SENTRY_DSN", sealed)
	assert.Error(t, err, "in another namespace")

	raw, err := base64.StdEncoding.DecodeString(sealed)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 0x01
	_, err = unsealKey(node, "garden", "SENTRY_DSN", base64.StdEncoding.EncodeToString(raw))
	assert.Error(t, err, "with one byte flipped")
}

func TestAKeysNameIsRefusedUnlessItReads(t *testing.T) {
	for _, name := range []string{"A", "OPENROUTER_API_KEY", "sentry.dsn", "a-b_c.9", strings.Repeat("k", 64)} {
		assert.Empty(t, keyNameRefused(name), name)
	}
	for _, name := range []string{"", strings.Repeat("k", 65), "two words", "a/b", "ké", "a=b", "a\nb"} {
		why := keyNameRefused(name)
		assert.NotEmpty(t, why, name)
		assert.Contains(t, why, name)
	}
}

func TestEveryLiveKeyUnseals(t *testing.T) {
	node := nodeKey(t)
	store, _ := createTestStore(t)
	at := time.Now().Add(-time.Minute)
	for i, name := range []string{"KEEP", "GONE"} {
		sealed, err := sealKey(node, "garden", name, []byte("value of "+name+"\n✻"))
		require.NoError(t, err)
		require.NoError(t, writeKey(store, "garden", rootAccount, name, map[string]any{"sealed": sealed, "set_by": rootAccount}, at.Add(time.Duration(i)*time.Second)))
	}
	require.NoError(t, writeKey(store, "garden", rootAccount, "GONE", map[string]any{"dropped": true, "set_by": rootAccount}, at.Add(10*time.Second)))

	keys, err := unsealedKeys(store, "garden", node)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"KEEP": "value of KEEP\n✻"}, keys)
}

// ownedNamespaces is a backend whose namespaces each have a store and, when
// owned, an ns.toml naming the owner.
type ownedNamespaces struct {
	stores map[string]ats.AttestationStore
	owners map[string]string
}

func (o ownedNamespaces) List() ([]storage.Namespace, error) {
	out := []storage.Namespace{{Name: auth.NamespaceDefault}}
	for name := range o.stores {
		ns := storage.Namespace{Name: name}
		if owner, ok := o.owners[name]; ok {
			ns.Definition = &storage.NamespaceDefinition{Owner: owner, Enabled: true}
		}
		out = append(out, ns)
	}
	return out, nil
}
func (ownedNamespaces) Create(string, storage.NamespaceDefinition) error { return nil }
func (ownedNamespaces) SetEnabled(string, bool) error                    { return nil }
func (ownedNamespaces) Delete(string) error                              { return nil }
func (ownedNamespaces) Nuke() error                                      { return nil }
func (o ownedNamespaces) OpenNamespace(name string) (*namespaces.Universe, error) {
	if s, ok := o.stores[name]; ok {
		return oneNamespace(name, s), nil
	}
	return nil, fmt.Errorf("no namespace %q served in test", name)
}

const gardenOwner = "https://mastodon.example/@gardener"

// keysServer is a node serving garden, owned by gardenOwner, and orchard, which
// nobody wrote an ns.toml for.
func keysServer(t *testing.T) (*QNTXServer, ats.AttestationStore) {
	t.Helper()
	s := rootKnowingServer(t)
	garden, _ := createTestStore(t)
	orchard, _ := createTestStore(t)
	backend := ownedNamespaces{
		stores: map[string]ats.AttestationStore{"garden": garden, "orchard": orchard},
		owners: map[string]string{"garden": gardenOwner},
	}
	s.held.SetKnown(backend)
	s.held.SetOpener(backend)
	s.nodeDID = &nodedid.Handler{PrivateKey: nodeKey(t)}
	return s, garden
}

// asking is a call from identity at a level, standing in namespace, carried
// on a request the way every surface carries one.
func asking(admitted auth.Admission, identity, namespace string) context.Context {
	admitted.Identity = identity
	admitted.Namespaces = []string{namespace}
	ctx := auth.WithAdmission(context.Background(), admitted)
	r := httptest.NewRequest(http.MethodGet, keysPath, nil).WithContext(ctx)
	return sigil.WithCaller(ctx, r)
}

func askKeys(t *testing.T, s *QNTXServer, ctx context.Context, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	signum := s.keysSignum()
	got, refused := signum.Answers[name](ctx, sent)
	if refused == nil {
		holds(t, signum, name, got)
	}
	return got, refused
}

func TestTheKeysSignumSaysWhatItHolds(t *testing.T) {
	require.NoError(t, (&QNTXServer{}).keysSignum().Check())
}

func TestTheOwnerSetsAKeyAndOnlyItsNameComesBack(t *testing.T) {
	s, garden := keysServer(t)
	owner := asking(auth.Holding(auth.Admitted(auth.LevelPublicRegistration), "GARDENER"), gardenOwner, "garden")

	got, refused := askKeys(t, s, owner, "set", sigil.Sent{"name": "OPENROUTER_API_KEY", "value": "sk-or-v1-secret"})
	require.Nil(t, refused, "%v", refused)
	body, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "sk-or-v1-secret")
	keys := got.(map[string]any)["keys"].([]keyListed)
	require.Len(t, keys, 1)
	assert.Equal(t, "OPENROUTER_API_KEY", keys[0].Name)
	assert.Equal(t, gardenOwner, keys[0].SetBy)

	found, err := garden.GetAttestations(ats.AttestationFilter{Subjects: []string{keySubject}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, []string{"OPENROUTER_API_KEY"}, found[0].Predicates)
	assert.Equal(t, []string{"_"}, found[0].Contexts)
	assert.Equal(t, gardenOwner, found[0].Actors[0])
	assert.Equal(t, pluginSource, found[0].Source)
	keysHeld, err := unsealedKeys(garden, "garden", s.nodeDID.PrivateKey)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"OPENROUTER_API_KEY": "sk-or-v1-secret"}, keysHeld)

	got, refused = askKeys(t, s, owner, "drop", sigil.Sent{"name": "OPENROUTER_API_KEY"})
	require.Nil(t, refused, "%v", refused)
	assert.Empty(t, got.(map[string]any)["keys"])
}

func TestATokenOrAConnectorIsRefusedAtAnyLevel(t *testing.T) {
	s, _ := keysServer(t)
	token := auth.Admitted(auth.LevelRoot)
	token.Grant = &auth.Grant{DID: "did:key:z6Mktoken"}
	connector := auth.Admitted(auth.LevelRoot)
	connector.ClientDID = "did:key:z6Mkclient"
	for _, admitted := range []auth.Admission{token, connector} {
		for _, name := range []string{"list", "set", "drop"} {
			_, refused := askKeys(t, s, asking(admitted, rootAccount, "garden"), name, sigil.Sent{"name": "K", "value": "v"})
			require.NotNil(t, refused, name)
			assert.Equal(t, sigil.NotAllowed, refused.GetWhy())
			assert.Contains(t, refused.GetSays(), "never by a token")
			assert.Contains(t, refused.GetSays(), "garden")
		}
	}
}

func TestSomebodyNotTheOwnerIsRefused(t *testing.T) {
	s, _ := keysServer(t)
	_, refused := askKeys(t, s, asking(auth.Holding(auth.Admitted(auth.LevelPublicRegistration), "GARDENER"), gardenerRoute, "garden"), "list", nil)
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "garden")

	// No ns.toml, default among them, is ROOT and SUPER only.
	for _, namespace := range []string{"orchard", auth.NamespaceDefault} {
		_, refused = askKeys(t, s, asking(auth.Holding(auth.Admitted(auth.LevelPublicRegistration), "GARDENER"), gardenOwner, namespace), "list", nil)
		require.NotNil(t, refused, namespace)
		assert.Contains(t, refused.GetSays(), namespace)
	}
	for _, level := range []auth.Level{auth.LevelRoot, auth.LevelSuper} {
		_, refused = askKeys(t, s, asking(auth.Admitted(level), rootAccount, "orchard"), "list", nil)
		assert.Nil(t, refused, "%s: %v", level, refused)
	}
}

func TestABadNameIsRefusedNamingIt(t *testing.T) {
	s, _ := keysServer(t)
	_, refused := askKeys(t, s, asking(auth.Admitted(auth.LevelRoot), rootAccount, "garden"), "set", sigil.Sent{"name": "two words", "value": "v"})
	require.NotNil(t, refused)
	assert.Equal(t, "name", refused.GetParam())
	assert.Contains(t, refused.GetSays(), "two words")
}
