package qntxinbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// heldStore is the store one call's token reaches, keeping what was written.
type heldStore struct {
	token   string
	written []*types.AsCommand
}

func (s *heldStore) GenerateAndCreateAttestation(_ context.Context, cmd *types.AsCommand) (*types.As, error) {
	s.written = append(s.written, cmd)
	return &types.As{ID: "AS-MAIL-1", Subjects: cmd.Subjects, Predicates: cmd.Predicates, Contexts: cmd.Contexts, Actors: cmd.Actors}, nil
}

func pluginWith(store *heldStore) *Plugin {
	p := NewPlugin()
	p.storeEndpoint = "localhost:50051"
	p.dial = func(_ context.Context, endpoint, token string) (attester, func() error, error) {
		store.token = token
		return store, func() error { return nil }, nil
	}
	return p
}

// asked is the request the node hands the plugin for one sigil call.
func asked(body, namespace string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/identity", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qntx-Asker", "did:key:z6MkTim")
	req.Header.Set("X-Qntx-Store-Token", "call")
	if namespace != "" {
		req.Header.Set("X-Qntx-Namespace", namespace)
	}
	return req
}

func serve(t *testing.T, p *Plugin, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	require.NoError(t, p.RegisterHTTP(mux))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// as <address> is mail:address of <User>, written by the asker the node named.
func TestAnAddressIsAttestedAsTheUsersByTheAsker(t *testing.T) {
	store := &heldStore{}
	w := serve(t, pluginWith(store), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, "Clean"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "call", store.token, "the write went with another token than the call's")
	require.Len(t, store.written, 1)
	written := store.written[0]
	assert.Equal(t, []string{"timothy@example.com"}, written.Subjects)
	assert.Equal(t, []string{"mail:address"}, written.Predicates)
	assert.Equal(t, []string{"US-TIM-7K4M3B9X"}, written.Contexts)
	assert.Equal(t, []string{"did:key:z6MkTim"}, written.Actors)
	assert.JSONEq(t, `{"created":{"id":"AS-MAIL-1","email":"timothy@example.com"}}`, w.Body.String())
}

// "it will also be namespace specific, and cant be system of default"
func TestNoAddressInSystemOrDefault(t *testing.T) {
	for _, namespace := range []string{"system", "default", ""} {
		store := &heldStore{}
		w := serve(t, pluginWith(store), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, namespace))
		assert.Equal(t, http.StatusForbidden, w.Code, "namespace %q: %s", namespace, w.Body.String())
		assert.Empty(t, store.written, "namespace %q took an address", namespace)
	}
}

// "A user can have multiple inboxes"
func TestAUserHoldsMoreThanOneAddress(t *testing.T) {
	store := &heldStore{}
	p := pluginWith(store)
	for _, email := range []string{"timothy@example.com", "contact@example.com"} {
		w := serve(t, p, asked(`{"user_id":"US-TIM-7K4M3B9X","email":"`+email+`"}`, "Clean"))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Len(t, store.written, 2)
	assert.Equal(t, []string{"US-TIM-7K4M3B9X"}, store.written[1].Contexts)
}

// Only a call the node handed can write: no token, no asker, no address.
func TestNoAddressWithoutTheNodesCall(t *testing.T) {
	for _, header := range []string{"X-Qntx-Store-Token", "X-Qntx-Asker"} {
		store := &heldStore{}
		req := asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, "Clean")
		req.Header.Del(header)
		w := serve(t, pluginWith(store), req)
		assert.Equal(t, http.StatusForbidden, w.Code, "without %s: %s", header, w.Body.String())
		assert.Empty(t, store.written, "without %s an address was written", header)
	}
}

func TestAnAddressNamesAUserAndIsAnAddress(t *testing.T) {
	for _, body := range []string{
		`{"email":"timothy@example.com"}`,
		`{"user_id":"US-TIM-7K4M3B9X"}`,
		`{"user_id":"US-TIM-7K4M3B9X","email":"example.com"}`,
	} {
		store := &heldStore{}
		w := serve(t, pluginWith(store), asked(body, "Clean"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", body, w.Body.String())
		assert.Empty(t, store.written, "%s was written", body)
	}
}

// "[At least 7 characters]@domain.tld"
func TestAnAddressHasAtLeastSevenCharactersBeforeTheAt(t *testing.T) {
	for email, status := range map[string]int{
		"abcdef@example.com":  http.StatusBadRequest,
		"abcdefg@example.com": http.StatusOK,
		"ëëëëëëë@example.com": http.StatusOK,
	} {
		store := &heldStore{}
		w := serve(t, pluginWith(store), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"`+email+`"}`, "Clean"))
		assert.Equal(t, status, w.Code, "%s: %s", email, w.Body.String())
	}
}

// QNTX makes the declared route a sigil and an MCP tool.
func TestThePluginDeclaresTheRoute(t *testing.T) {
	routes := NewPlugin().DeclaredRoutes()
	require.Len(t, routes, 1)
	assert.Equal(t, http.MethodPost, routes[0].GetMethod())
	assert.Equal(t, "/identity", routes[0].GetPath())
}

var _ protocol.InboxServiceServer = (*Plugin)(nil)
