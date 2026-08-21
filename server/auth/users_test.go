package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// memUsers is the UserStore contract with nothing behind it. The parquet
// implementation is tested in crates/ats-duckdb; what is tested here is who
// gets minted and when, which is policy rather than storage.
type memUsers struct {
	held []User
}

func (m *memUsers) ByRoute(route string) (User, bool, error) {
	for _, u := range m.held {
		if u.Reaches(route) {
			return u, true, nil
		}
	}
	return User{}, false, nil
}

func (m *memUsers) List() ([]User, error) { return m.held, nil }

func (m *memUsers) Put(u User) error {
	for i, held := range m.held {
		if held.ID == u.ID {
			m.held[i] = u
			return nil
		}
	}
	m.held = append(m.held, u)
	return nil
}

func handlerWithUsers(t *testing.T) (*Handler, *memUsers) {
	t.Helper()
	store := &memUsers{}
	return &Handler{users: store, logger: zap.NewNop().Sugar()}, store
}

// The binding an account route arrives with. Only the fields a User reads.
func accountBinding(provider, canonicalID, handle string) *SignedBinding {
	b := &SignedBinding{}
	b.Claim.Provider = provider
	b.Claim.CanonicalID = canonicalID
	b.Claim.Handle = &handle
	return b
}

func mastodonBinding(handle string) *SignedBinding {
	return accountBinding("mastodon", mastodonAccount, handle)
}

// Proving a listed route is what creates the first User, and the first User is
// always the ROOT User. There is no separate act.
func TestTheFirstProvenRouteMintsRoot(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowserOne")

	u, found, err := store.ByRoute(mastodonAccount)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, LevelRoot, u.Level)
	assert.Empty(t, u.CreatedBy, "ROOT names the node that admitted it, and that is not built yet")
	require.Len(t, u.Accounts, 1)
	assert.Equal(t, "mastodon", u.Accounts[0].Provider)
	assert.Equal(t, "@tim@mastodon.example", u.Accounts[0].Handle)
}

// A User is not the browser it logged in from. laye mints a key per browser and
// a phone is a browser, so every session a person opens is one more key on the
// same record rather than one more person.
func TestEveryBrowserJoinsTheSameUser(t *testing.T) {
	h, store := handlerWithUsers(t)

	sessions := []string{
		"did:key:zLaptopFirefox",
		"did:key:zLaptopChrome",
		"did:key:zPhone",
		"did:key:zSecondPhone",
		"did:key:zTablet",
	}
	for _, did := range sessions {
		h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), did)
	}

	require.Len(t, store.held, 1, "the same route reached five times is one User")

	dids := []string{}
	for _, k := range store.held[0].Keys {
		assert.Equal(t, OriginBrowser, k.Origin)
		dids = append(dids, k.DID)
	}
	assert.ElementsMatch(t, sessions, dids)
}

// A browser that logs in again is not a third key.
func TestTheSameBrowserIsNotRecordedTwice(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowserOne")
	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowserOne")

	require.Len(t, store.held, 1)
	assert.Len(t, store.held[0].Keys, 1)
}

// A did:key entry needs no binding, and the key that proved itself is the
// route. It is written down as a key rather than as an account.
func TestADidKeyRouteIsRecordedAsAKey(t *testing.T) {
	h, store := handlerWithUsers(t)
	const route = "did:key:zStandsAlone"

	h.joinUser(route, nil, route)

	u, found, err := store.ByRoute(route)
	require.NoError(t, err)
	require.True(t, found)
	assert.Empty(t, u.Accounts)
	require.Len(t, u.Keys, 1, "the route is the key, not a second entry")
	assert.Equal(t, route, u.Keys[0].DID)
}

// There is one ROOT User, and listing several routes is how that one User is
// reached from more than one place. A person holds accounts at as many
// providers as they like, and every listed route reaches the same record.
func TestEveryListedRouteReachesOneUser(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	h.joinUser("did:plc:tim", accountBinding("atproto", "did:plc:tim", "@tim.bsky.social"), "did:key:zBrowser")
	h.joinUser("https://github.com/tim", accountBinding("github", "https://github.com/tim", "@tim"), "did:key:zBrowser")
	h.joinUser("did:key:zTimsOwnKey", nil, "did:key:zBrowser")

	require.Len(t, store.held, 1, "several providers is still one person")
	u := store.held[0]
	assert.Equal(t, LevelRoot, u.Level)

	assert.True(t, u.Reaches(mastodonAccount))
	assert.True(t, u.Reaches("did:plc:tim"))
	assert.True(t, u.Reaches("https://github.com/tim"))
	assert.True(t, u.Reaches("did:key:zTimsOwnKey"), "a key of their own is a route too")
}

// Nothing is keyed by provider, so a second account at the same one is a second
// account rather than a replacement. Two instances, one person.
func TestTwoAccountsAtTheSameProviderBothLand(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	h.joinUser("https://other.example/@tim",
		accountBinding("mastodon", "https://other.example/@tim", "@tim@other.example"), "did:key:zBrowser")

	require.Len(t, store.held, 1)
	require.Len(t, store.held[0].Accounts, 2)

	handles := []string{}
	for _, a := range store.held[0].Accounts {
		assert.Equal(t, "mastodon", a.Provider)
		handles = append(handles, a.Handle)
	}
	assert.ElementsMatch(t, []string{"@tim@mastodon.example", "@tim@other.example"}, handles)
}

// A key an authenticator derived is a device, not a browser. Both are keys the
// same User holds, and what tells them apart is where they came from.
func TestEnrollingADeviceRecordsItAsADevice(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zPhoneBrowser")
	h.joinDeviceKey(mastodonAccount, "did:key:zPhoneFinger")

	require.Len(t, store.held, 1)
	origins := map[string]string{}
	for _, k := range store.held[0].Keys {
		origins[k.DID] = k.Origin
	}
	assert.Equal(t, OriginBrowser, origins["did:key:zPhoneBrowser"])
	assert.Equal(t, OriginDevice, origins["did:key:zPhoneFinger"])
}

// The same finger derives the same key, so enrolling again is not a second one.
func TestEnrollingTheSameDeviceTwiceIsOneKey(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	h.joinDeviceKey(mastodonAccount, "did:key:zFinger")
	h.joinDeviceKey(mastodonAccount, "did:key:zFinger")

	require.Len(t, store.held, 1)
	assert.Len(t, store.held[0].Keys, 2, "one browser and one device")
}

// Every phone a person enrols is another device key on the one record.
func TestEveryDeviceJoinsTheSameUser(t *testing.T) {
	h, store := handlerWithUsers(t)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	for _, did := range []string{"did:key:zLaptopTouchID", "did:key:zPhoneFaceID", "did:key:zYubikey"} {
		h.joinDeviceKey(mastodonAccount, did)
	}

	require.Len(t, store.held, 1)
	devices := 0
	for _, k := range store.held[0].Keys {
		if k.Origin == OriginDevice {
			devices++
		}
	}
	assert.Equal(t, 3, devices)
}

// The User is resolved once, when the session is made, so no request after it
// has to scan the store to know who is asking.
func TestASessionCarriesTheUser(t *testing.T) {
	h, store := handlerWithUsers(t)
	h.sessions = newSessionStore(24)

	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	require.Len(t, store.held, 1)
	store.held[0].Username = "tim"

	token, err := h.sessions.create(mastodonAccount, h.userFor(mastodonAccount))
	require.NoError(t, err)

	userID, username := h.sessions.userOf(token)
	assert.Equal(t, store.held[0].ID, userID)
	assert.Equal(t, "tim", username)
}

// A deployment that keeps no Users still issues sessions. They name a route and
// nobody, which is what the row falls back to drawing.
func TestASessionWithoutAUserStoreNamesNobody(t *testing.T) {
	h := &Handler{sessions: newSessionStore(24), logger: zap.NewNop().Sugar()}

	token, err := h.sessions.create(mastodonAccount, h.userFor(mastodonAccount))
	require.NoError(t, err)

	userID, username := h.sessions.userOf(token)
	assert.Empty(t, userID)
	assert.Empty(t, username)
}

// A backend with no User store records nothing. An admission that worked is not
// undone by there being nowhere to write it down.
func TestAdmissionSurvivesHavingNoUserStore(t *testing.T) {
	h := &Handler{logger: zap.NewNop().Sugar()}

	assert.NotPanics(t, func() {
		h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowserOne")
	})
}
