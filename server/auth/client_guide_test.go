package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "guide your agent is easy to add"
//
// A client's instructions are what its connector's MCP server tells the
// agent on initialize (ADR-038).

func guideRequest(id, body, session string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens/"+id+"/instructions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	return req
}

func TestAClientIsMintedWithInstructions(t *testing.T) {
	h, store := grantHandler(t)

	rec := mintClient(t, h, `{"expires_at":"never","label":"app","level":"OAUTH","namespaces":["default"],"return_address":"https://app.example/callback","instructions":"Always show drafts before sending."}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Token        string `json:"token"`
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Always show drafts before sending.", resp.Instructions)

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	client, ok := h.clientByDID(grant.DID)
	require.True(t, ok)
	assert.Equal(t, "Always show drafts before sending.", client.Instructions)
	assert.Equal(t, "Always show drafts before sending.", h.ConnectorInstructions(grant.DID))
}

func TestOnlyAClientHasInstructions(t *testing.T) {
	h, _ := grantHandler(t)

	rec := mintClient(t, h, `{"expires_at":"never","label":"ingest","level":"ATTESTOR","namespaces":["default"],"instructions":"Be brief."}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "only a client has instructions")
}

func TestInstructionsAreBounded(t *testing.T) {
	h, _ := grantHandler(t)
	long := strings.Repeat("x", maxInstructionsRunes+1)

	rec := mintClient(t, h, `{"expires_at":"never","label":"app","level":"OAUTH","namespaces":["default"],"return_address":"https://app.example/callback","instructions":"`+long+`"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "5000")
}

// Changed at will, and a connector reads the client at its next initialize.
func TestRootChangesAClientsInstructions(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)
	assert.Equal(t, "", h.ConnectorInstructions(did))

	rec := httptest.NewRecorder()
	byID(h, rec, guideRequest(id, `{"instructions":"Keep replies concise."}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Keep replies concise.", h.ConnectorInstructions(did))

	cleared := httptest.NewRecorder()
	byID(h, cleared, guideRequest(id, `{"instructions":""}`, session))
	require.Equal(t, http.StatusOK, cleared.Code, cleared.Body.String())
	assert.Equal(t, "", h.ConnectorInstructions(did))
}

func TestOnlyAClientIsGuided(t *testing.T) {
	h, store := grantHandler(t)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	_, id, err := store.Create(NewToken{Label: "cron", MintedBy: mastodonAccount, Level: LevelAttestor, Namespaces: []string{NamespaceDefault}})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	byID(h, rec, guideRequest(id, `{"instructions":"Be brief."}`, session))

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// A DID no live client answers to guides nobody.
func TestNoClientNoInstructions(t *testing.T) {
	h, _ := grantHandler(t)
	assert.Equal(t, "", h.ConnectorInstructions("did:key:znobody"))
}
