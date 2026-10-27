package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A connector reaches what the lines grant its client's label, and nothing on
// its own (ADR-038). No line, no reach (ADR-034):
//
// "DEFAULT DENY"

// connectorToken is a connector's access token through the minted client, as
// the flow issues it.
func connectorToken(t *testing.T, h *Handler, store *memTokenStore, did string) string {
	t.Helper()
	code, verifier := codeFor(t, h, did)
	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, clientSecret(t, store, did), code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	return answer.AccessToken
}

func admittedWith(t *testing.T, h *Handler, raw string) Admission {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	admission, admitted := h.admissionOf(h.presented(req))
	require.True(t, admitted)
	return admission
}

// The client is named "app". A grant to app in its namespace is what the
// connector holds, and the role's words are its words.
func TestAConnectorHoldsWhatItsClientsLabelIsGranted(t *testing.T) {
	h, store, did := authorizingHandler(t)
	raw := connectorToken(t, h, store, did)

	kept := &memRoles{lines: map[string][]RoleLine{}}
	h.SetRoleReader(kept)
	nobody := admittedWith(t, h, raw)
	assert.Equal(t, string(LevelConnector), nobody.LevelName())
	assert.Empty(t, nobody.Roles())
	assert.False(t, nobody.ReachesAStore(), "a connector holding nothing reaches a store")
	assert.False(t, nobody.MaySeeSystem())

	kept.lines[NamespaceDefault] = []RoleLine{{Routes: []string{"app"}, Roles: []string{"APP"}, Granted: true, Actor: mastodonAccount, At: time.Now()}}
	kept.words = []WordLine{{Write: true, Words: []string{"note:taken"}, Roles: []string{"APP"}, Actor: mastodonAccount, At: time.Now()}}
	h.ForgetRoles()
	app := admittedWith(t, h, raw)
	assert.Equal(t, []string{"APP"}, app.Roles())
	assert.True(t, app.ReachesAStore())
	assert.True(t, app.MayWrite("note:taken"))
	assert.False(t, app.MayWrite("note:lost"), "a connector wrote a word no line granted")
	assert.False(t, app.MayRead("note:taken"), "a connector read with no READ line")
	assert.Equal(t, mastodonAccount, app.Identity, "the connector stopped being the person")
	assert.Equal(t, did, app.ClientDID)
}

// A rung no line names: no path is a connector's by its level, however the
// person stands.
func TestAConnectorIsNotRootHoweverThePersonStands(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.Equal(t, LevelRoot, h.levelOf(mastodonAccount))
	raw := connectorToken(t, h, store, did)

	admission := admittedWith(t, h, raw)
	assert.False(t, admission.IsRoot())
	assert.False(t, Also(LevelRoot, LevelSuper, LevelToken, LevelAttestor, LevelUser).Admits(admission),
		"a line naming every rung but CONNECTOR admitted a connector")
	assert.True(t, Reach{}.AndRoles("APP").Admits(Holding(admission, "APP")))
}
