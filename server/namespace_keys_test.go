package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/encoding/protojson"
)

// keysAs is a call on the keys signum by an admission, acting in the
// namespaces it names.
func keysAs(s *QNTXServer, admitted auth.Admission, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/keys/"+sent["namespace"], nil)
	ctx := sigil.WithCaller(auth.WithAdmission(context.Background(), admitted), asked)
	return s.keysSignum().Answers[name](ctx, sent)
}

func person(who string, level auth.Level, acting ...string) auth.Admission {
	admitted := auth.Admitted(level, acting...)
	admitted.Identity = who
	return admitted
}

// "No sigil answers a value": the owner sets a key, keys list names it and who
// set it, and what is kept is sealed, its value nowhere in the line.
func TestANamespacesKeyIsSetSealedAndNamedNotAnswered(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)
	owner := person(gardener, auth.LevelUser, "garden")

	set, refused := keysAs(s, owner, "set", sigil.Sent{"namespace": "garden", "name": "openrouter", "value": "sk-or-the-gardeners"})
	require.Nil(t, refused)
	holds(t, s.keysSignum(), "set", set)
	keys := set.(*protocol.NamespaceKeys)
	require.Len(t, keys.GetKeys(), 1)
	assert.Equal(t, "openrouter", keys.GetKeys()[0].GetName())
	assert.Equal(t, gardener, keys.GetKeys()[0].GetSetBy())

	listed, refused := keysAs(s, owner, "list", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	holds(t, s.keysSignum(), "list", listed)
	written, err := protojson.Marshal(listed.(*protocol.NamespaceKeys))
	require.NoError(t, err)
	assert.NotContains(t, string(written), "sk-or-the-gardeners", "a value is never answered")

	for _, row := range in.store["garden"].rows {
		for _, value := range row.Attributes {
			text, isText := value.(string)
			assert.False(t, isText && strings.Contains(text, "sk-or-the-gardeners"), "the value is kept sealed: %v", row.Attributes)
		}
	}
	value, err := s.keyValue("garden", "openrouter")
	require.NoError(t, err)
	assert.Equal(t, "sk-or-the-gardeners", value, "the node unseals it for the harness it starts")
	_, err = s.keyValue("orchard", "openrouter")
	assert.Error(t, err, "nothing crosses: orchard holds no key garden set")

	dropped, refused := keysAs(s, owner, "drop", sigil.Sent{"namespace": "garden", "name": "openrouter"})
	require.Nil(t, refused)
	holds(t, s.keysSignum(), "drop", dropped)
	assert.Empty(t, dropped.(*protocol.NamespaceKeys).GetKeys())
	_, err = s.keyValue("garden", "openrouter")
	assert.ErrorContains(t, err, "dropped")
}

// "a model never lists, sets or reads a key": a token and a connector are
// refused at any level, and somebody who merely acts in the namespace is too.
func TestANamespacesKeysAreAPersonsToHandle(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)

	token := person("https://example.org/root", auth.LevelRoot)
	token.Grant = &access.Grant{}
	_, refused := keysAs(s, token, "list", sigil.Sent{"namespace": "garden"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.Why)
	assert.Contains(t, refused.Says, "a token asked")

	connector := person(gardener, auth.LevelUser, "garden")
	connector.ClientDID = "did:key:z6MkTheConnector"
	_, refused = keysAs(s, connector, "set", sigil.Sent{"namespace": "garden", "name": "openrouter", "value": "sk"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.Why)
	assert.Contains(t, refused.Says, "connector")

	_, refused = keysAs(s, person(visitor, auth.LevelUser, "garden"), "list", sigil.Sent{"namespace": "garden"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotAllowed, refused.Why)

	_, refused = keysAs(s, person("https://example.org/super", auth.LevelSuper), "set", sigil.Sent{"namespace": "orchard", "name": "anthropic", "value": "sk-ant"})
	assert.Nil(t, refused, "SUPER handles a namespace's keys")
}

// providerIssuing answers as a provider does for the one key it issued, and
// 401 for any other, on every path a key is asked at.
func providerIssuing(t *testing.T, issued string) string {
	t.Helper()
	at := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == issued || r.Header.Get("Authorization") == "Bearer "+issued {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid x-api-key"}`))
	}))
	t.Cleanup(at.Close)
	return at.URL
}

// "kept with red light"
func TestAProvidersKeyIsKeptAndSaysWhetherItsProviderAcceptsIt(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	s.providersAt = map[string]string{"anthropic": providerIssuing(t, "sk-ant-issued")}
	owner := person(gardener, auth.LevelUser, "garden")

	set, refused := keysAs(s, owner, "set", sigil.Sent{"namespace": "garden", "name": "anthropic", "value": "sk-ant-mistyped"})
	require.Nil(t, refused, "a key its provider refuses is kept")
	red := set.(*protocol.NamespaceKeys).GetKeys()[0]
	assert.True(t, red.GetAsked())
	assert.False(t, red.GetAccepted())
	assert.Contains(t, red.GetWhy(), "401")

	_, refused = keysAs(s, owner, "set", sigil.Sent{"namespace": "garden", "name": "anthropic", "value": "sk-ant-issued"})
	require.Nil(t, refused)
	listed, refused := keysAs(s, owner, "list", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	green := listed.(*protocol.NamespaceKeys).GetKeys()[0]
	assert.True(t, green.GetAsked())
	assert.True(t, green.GetAccepted())
	assert.Empty(t, green.GetWhy())

	_, refused = keysAs(s, owner, "set", sigil.Sent{"namespace": "garden", "name": "sentry", "value": "a-dsn"})
	require.Nil(t, refused)
	listed, refused = keysAs(s, owner, "list", sigil.Sent{"namespace": "garden"})
	require.Nil(t, refused)
	for _, key := range listed.(*protocol.NamespaceKeys).GetKeys() {
		if key.GetName() == "sentry" {
			assert.False(t, key.GetAsked())
		}
	}
}

// The keys paths let in whoever acts in a namespace and no token: who handles
// them is the handler's to ask.
func TestTheTableGivesAnamespacesKeysToNoToken(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _, _ := runningNamespaceAgents(t)
	for _, held := range s.keysSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT", "SUPER", "USER"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
	require.NoError(t, s.keysSignum().Check())
}
