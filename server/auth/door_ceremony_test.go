package auth

import (
	"encoding/base64"
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

// credentialsOffered digs allowCredentials out of the options a login hands
// the browser: the keys it may answer with.
func credentialsOffered(t *testing.T, body []byte) []string {
	t.Helper()
	var options struct {
		PublicKey struct {
			AllowCredentials []struct {
				ID string `json:"id"`
			} `json:"allowCredentials"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(body, &options))
	ids := make([]string, 0, len(options.PublicKey.AllowCredentials))
	for _, c := range options.PublicKey.AllowCredentials {
		ids = append(ids, c.ID)
	}
	return ids
}

// A login is offered the devices of the person laye admitted, not every key
// at the door. Where the deployment keeps no Users, the person is the route
// alone. Somebody else's device sits at the same door and is not offered.
func TestALoginIsOfferedTheAdmittedIdentitysDevices(t *testing.T) {
	h := handlerWithDoors(t, garden())
	require.NoError(t, h.creds.saveAt(credential("laptop"), "did:key:zlaptop", mastodonAccount, NamespaceDefault))
	require.NoError(t, h.creds.saveAt(credential("phone"), "did:key:zphone", "apple:001750", NamespaceDefault))

	offered := ceremonyAt(t, h, "/auth/login/begin", "https://q.sbvh.nl")
	require.Equal(t, http.StatusOK, offered.Code, offered.Body.String())
	assert.Equal(t, []string{base64.RawURLEncoding.EncodeToString([]byte("laptop"))},
		credentialsOffered(t, offered.Body.Bytes()),
		"a login admitted as one identity was offered another identity's device")
}

// A device belongs to the person, not to the route they came in by. The laptop
// enrolled under the google route and the phone under the apple route are two
// devices of one User, so a login by either route is offered both.
//
// "Both are apple, I use a MacBook Pro." "Should be same identity, same user though."
func TestALoginIsOfferedEveryDeviceOfTheUser(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.users = &memUsers{held: []User{{ID: "US-ONE", Level: LevelRoot, Accounts: []UserAccount{
		{Provider: "mastodon", CanonicalID: mastodonAccount},
		{Provider: "apple", CanonicalID: "apple:001750"},
	}}}}
	require.NoError(t, h.creds.saveAt(credential("laptop"), "did:key:zlaptop", mastodonAccount, NamespaceDefault))
	require.NoError(t, h.creds.saveAt(credential("phone"), "did:key:zphone", "apple:001750", NamespaceDefault))
	require.NoError(t, h.creds.saveAt(credential("stranger"), "did:key:zother", "google:stranger", NamespaceDefault))

	offered := ceremonyAt(t, h, "/auth/login/begin", "https://q.sbvh.nl")
	require.Equal(t, http.StatusOK, offered.Code, offered.Body.String())
	assert.ElementsMatch(t, []string{
		base64.RawURLEncoding.EncodeToString([]byte("laptop")),
		base64.RawURLEncoding.EncodeToString([]byte("phone")),
	}, credentialsOffered(t, offered.Body.Bytes()),
		"a login was offered something other than the person's own devices")

	held, err := h.hasDevice("apple:001750")
	require.NoError(t, err)
	assert.True(t, held)
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
