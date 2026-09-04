package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// halfAdmitted is the cookie laye leaves behind. Both ceremonies stand on one,
// so every request below carries it.
func halfAdmitted(t *testing.T, h *Handler) *http.Cookie {
	t.Helper()
	pending, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)
	return &http.Cookie{Name: pendingCookieName, Value: pending}
}

func ceremonyAt(t *testing.T, h *Handler, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.Header.Set("Origin", origin)
	r.AddCookie(halfAdmitted(t, h))

	w := httptest.NewRecorder()
	switch path {
	case "/auth/register/begin":
		h.handleRegisterBegin(w, r)
	case "/auth/login/begin":
		h.handleLoginBegin(w, r)
	default:
		t.Fatalf("no ceremony at %s", path)
	}
	return w
}

// rpOffered digs the relying party out of the options a ceremony hands the
// browser. That id is what the authenticator hashes, so it is the whole of
// which door the ceremony is running against.
func rpOffered(t *testing.T, body []byte) string {
	t.Helper()
	var options struct {
		PublicKey struct {
			RP struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(body, &options))
	return options.PublicKey.RP.ID
}

// A registration runs against the relying party of the door it arrived at.
// Running it against the node's own would hand the browser an rp id it refuses.
func TestARegistrationRunsAgainstTheDoorItArrivedAt(t *testing.T) {
	h := handlerWithDoors(t, garden())

	atVak := ceremonyAt(t, h, "/auth/register/begin", "https://portal.garden.test")
	require.Equal(t, http.StatusOK, atVak.Code, atVak.Body.String())
	assert.Equal(t, "garden.test", rpOffered(t, atVak.Body.Bytes()))

	atOwn := ceremonyAt(t, h, "/auth/register/begin", "https://q.sbvh.nl")
	require.Equal(t, http.StatusOK, atOwn.Code, atOwn.Body.String())
	assert.Equal(t, "q.sbvh.nl", rpOffered(t, atOwn.Body.Bytes()))
}

// A login at one door is offered the keys made there. A door nobody has
// registered at has none, even when the node is full of credentials from
// somewhere else.
func TestALoginSeesOnlyItsOwnDoorsCredentials(t *testing.T) {
	h := handlerWithDoors(t, garden())
	require.NoError(t, h.creds.saveAt(credential("at-default"), "did:key:zone", mastodonAccount, NamespaceDefault))

	atOwn := ceremonyAt(t, h, "/auth/login/begin", "https://q.sbvh.nl")
	assert.Equal(t, http.StatusOK, atOwn.Code, atOwn.Body.String())

	atVak := ceremonyAt(t, h, "/auth/login/begin", "https://portal.garden.test")
	assert.Equal(t, http.StatusBadRequest, atVak.Code,
		"a door with no registrations was offered another door's keys")
}

// An origin no door claims reaches no ceremony, and is told nothing about why.
func TestACeremonyFromAnUnclaimedOriginIsRefused(t *testing.T) {
	h := handlerWithDoors(t, garden())
	require.NoError(t, h.creds.saveAt(credential("at-default"), "did:key:zone", mastodonAccount, NamespaceDefault))

	for _, path := range []string{"/auth/register/begin", "/auth/login/begin"} {
		refused := ceremonyAt(t, h, path, "https://somewhere.else.example")
		assert.Equal(t, http.StatusUnauthorized, refused.Code, path)
		assert.NotContains(t, refused.Body.String(), "garden",
			"a refusal named a door the caller never reached")
	}
}
