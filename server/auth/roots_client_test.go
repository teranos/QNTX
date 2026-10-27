package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "but i want a MCP specifically for the Node, only for ROOT"
//
// ROOT's client is minted as one, and its MCP server is the node's (ADR-038).

// rootsClient writes ROOT's client beside the one authorizingHandler wrote,
// and answers its DID.
func rootsClient(t *testing.T, store *memTokenStore) string {
	t.Helper()
	_, _, err := store.Create(NewToken{
		Label: "root", MintedBy: mastodonAccount, Level: LevelOAuth, ReturnAddress: appReturn, Root: true,
	})
	require.NoError(t, err)
	listed, err := store.List()
	require.NoError(t, err)
	for _, info := range listed {
		if info.Label == "root" {
			return info.DID
		}
	}
	t.Fatal("ROOT's client was not written")
	return ""
}

func TestROOTMintsROOTsClient(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	rec := mintClient(t, h, `{"expires_at":"never","label":"root","level":"OAUTH","root":true,"return_address":"https://app.example/callback"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].Root)
	assert.Empty(t, listed[0].Namespaces)

	// "AND I SHOULD NOT NEED TO OPEN UP THE TOKEN ELEMENT SEPARATELY TO CREATE THE TOKEN"
	//
	// The client id an app is set up with is said where it is minted.
	var resp struct {
		DID string `json:"did"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, listed[0].DID, resp.DID)
}

func TestROOTsClientIsNobodyElsesToMint(t *testing.T) {
	h, store := grantHandler(t)

	rec := mintClient(t, h, `{"expires_at":"never","label":"root","level":"OAUTH","root":true,"return_address":"https://app.example/callback"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	listed, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, listed, "somebody not ROOT minted ROOT's client")
}

// Its MCP server is /mcp, which names no namespace.
func TestROOTsClientNamesNoNamespace(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	rec := mintClient(t, h, `{"expires_at":"never","label":"root","level":"OAUTH","root":true,"namespaces":["default"],"return_address":"https://app.example/callback"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// "as ROOT, everything ROOT reaches"
func TestROOTsClientsTokenIsROOT(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	did := rootsClient(t, store)
	raw := connectorToken(t, h, store, did)

	admission := admittedWith(t, h, raw)
	assert.True(t, admission.IsRoot(), "ROOT's client's token is %s", admission.LevelName())
	assert.Equal(t, did, admission.ClientDID)
}

// Only ROOT says yes for ROOT's client.
func TestROOTsClientIsSentNoCodeForSomebodyNotROOT(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	did := rootsClient(t, store)

	_, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	next := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)

	// Who said yes is no longer ROOT by the time the code would be issued.
	h.SetIdentities([]string{"did:key:zsomebodyelse"}, nil)

	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	assert.NotEqual(t, http.StatusSeeOther, done.Code, "a code went to ROOT's client for somebody not ROOT")
	if sent, err := url.Parse(done.Header().Get("Location")); err == nil {
		assert.Empty(t, sent.Query().Get("code"))
	}
}

// "THIS IS MY TOKEN"
func TestROOTsClientsOwnTokenIsROOT(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	did := rootsClient(t, store)

	w := httptest.NewRecorder()
	h.handleToken(w, clientCredentialsRequest(did, clientSecret(t, store, did)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))

	admission := admittedWith(t, h, answer.AccessToken)
	assert.True(t, admission.IsRoot(), "ROOT's client's own token is %s", admission.LevelName())
}

// ROOT's, and nobody's once its ROOT is not ROOT.
func TestROOTsClientsTokenIsNobodyOnceItsPersonIsNotROOT(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	did := rootsClient(t, store)
	raw := connectorToken(t, h, store, did)

	h.SetIdentities([]string{"did:key:zsomebodyelse"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	_, admitted := h.admissionOf(h.presented(req))
	assert.False(t, admitted, "ROOT's client's token outlived its ROOT")
}
