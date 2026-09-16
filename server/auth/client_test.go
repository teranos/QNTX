package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client is a door (ADR-025): both ends are the same hand. ROOT mints it
// with the return address its codes go to, the way ROOT writes a door's
// origin in am.toml.

func mintClient(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(body, session))
	return rec
}

func TestAClientIsMintedWithAReturnAddress(t *testing.T) {
	h, store := grantHandler(t)

	rec := mintClient(t, h, `{"label":"app","level":"OAUTH","return_address":"https://app.example/callback"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Token         string `json:"token"`
		Level         string `json:"level"`
		ReturnAddress string `json:"return_address"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "OAUTH", resp.Level)
	assert.Equal(t, "https://app.example/callback", resp.ReturnAddress)

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Equal(t, LevelOAuth, grant.Level)
	assert.Equal(t, "https://app.example/callback", grant.ReturnAddress)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
}

// A client is bound to the door it was minted at. A session at the node's own
// door is in default (ADR-032).
func TestAClientIsBoundToTheDoorItWasMintedAt(t *testing.T) {
	h, store := grantHandler(t)

	rec := mintClient(t, h, `{"label":"app","level":"OAUTH","return_address":"https://app.example/callback"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, []string{NamespaceDefault}, listed[0].Namespaces)
}

func TestAClientNamesNoNamespace(t *testing.T) {
	h, _ := grantHandler(t)

	rec := mintClient(t, h, `{"label":"app","level":"OAUTH","return_address":"https://app.example/callback","namespaces":["pond"]}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestAClientWithoutAReturnAddressIsRefused(t *testing.T) {
	h, _ := grantHandler(t)

	rec := mintClient(t, h, `{"label":"app","level":"OAUTH"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// A code is sent to the address whole, so the address has to be one a code
// can reach: absolute, and without a fragment, which never leaves the browser.
func TestAReturnAddressHasToBeReachable(t *testing.T) {
	for name, address := range map[string]string{
		"relative":     "/callback",
		"no scheme":    "app.example/callback",
		"fragment":     "https://app.example/callback#code",
		"only a word":  "callback",
		"empty scheme": "://app.example/callback",
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := grantHandler(t)
			rec := mintClient(t, h, `{"label":"app","level":"OAUTH","return_address":"`+address+`"}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestOnlyAClientHasAReturnAddress(t *testing.T) {
	h, _ := grantHandler(t)

	rec := mintClient(t, h, `{"label":"ingest","level":"ATTESTOR","return_address":"https://app.example/callback"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// A client authenticates at the token endpoint and nowhere else. The secret
// an app holds must not be a bearer that reaches a route, whoever minted it.
func TestAClientIsNotABearer(t *testing.T) {
	store := newMemTokenStore()
	h := &Handler{
		sessions: newSessionStore(1),
		tokens:   store,
		logger:   testLogger(),
	}
	h.SetIdentities([]string{mastodonAccount}, nil)

	client, _, err := store.Create(NewToken{
		Label: "app", MintedBy: mastodonAccount, Level: LevelOAuth,
		Namespaces: []string{NamespaceDefault}, ReturnAddress: "https://app.example/callback",
	})
	require.NoError(t, err)
	// The same minter, the same reach: only the kind differs.
	token, _, err := store.Create(NewToken{
		Label: "cron", MintedBy: mastodonAccount, Level: LevelAttestor,
		Namespaces: []string{NamespaceDefault},
	})
	require.NoError(t, err)

	reached := 0
	guarded := h.Middleware("/test", Also(LevelAttestor, LevelOAuth), func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})

	asBearer := func(raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		guarded(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, asBearer(token).Code)
	assert.Equal(t, 1, reached)

	rec := asBearer(client)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "a client is not a bearer")
	assert.Equal(t, 1, reached, "the client reached the route")
}
