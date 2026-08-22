package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The fictional account the rest of the auth tests use.
const rootProfile = mastodonAccount

func setupHandler(t *testing.T, roots []string, users UserStore) *Handler {
	t.Helper()
	h := &Handler{users: users, logger: zap.NewNop().Sugar()}
	h.SetIdentities(roots, nil)
	return h
}

func setupBody(t *testing.T, h *Handler) (SetupState, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleSetup(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var state SetupState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &state))
	return state, rec.Body.String()
}

// A profile URL carries its own host, which is the whole reason nobody types
// an instance on a node that already lists one.
func TestAProfileUrlCarriesItsHost(t *testing.T) {
	got, ok := claimable(rootProfile)
	require.True(t, ok)
	assert.Equal(t, "mastodon.example", got.host)
	assert.Equal(t, "mastodon", got.provider)
}

// An entry this cannot read is still a way in, just not a single press.
func TestUnreadableRoutesAreNotOffered(t *testing.T) {
	for _, route := range []string{
		"did:plc:someonesplcidentifier",
		"did:key:z6MkjJ2uVRt52Moic76yk3XLypKXU6htuYXKAzL1d8n2YPxK",
		"http://mastodon.example/@tim",
		"https://mastodon.example",
		"https://mastodon.example/users/tim",
		"https://mastodon.example/@tim/statuses/1",
	} {
		if _, ok := claimable(route); ok {
			t.Fatalf("%q was offered as a one-press claim", route)
		}
	}
}

// The one that matters: a page anyone can load must not say who owns the box.
func TestSetupNamesTheMethodAndNeverTheOwner(t *testing.T) {
	h := setupHandler(t, []string{rootProfile, "did:plc:xyz"}, &memUsers{})

	state, raw := setupBody(t, h)
	assert.False(t, state.Claimed)
	assert.True(t, state.Governed)

	require.Len(t, state.Methods, 1)
	assert.Equal(t, "mastodon", state.Methods[0].Provider)
	assert.Equal(t, "Mastodon", state.Methods[0].Label)

	for _, leak := range []string{"tim", "mastodon.example", "did:plc:xyz", rootProfile} {
		if strings.Contains(raw, leak) {
			t.Fatalf("setup leaked %q to an unauthenticated caller: %s", leak, raw)
		}
	}
}

// Two accounts at one provider are one way in. Counting them would say how
// many people this node lists.
func TestTwoAccountsAtOneProviderAreOneMethod(t *testing.T) {
	h := setupHandler(t, []string{rootProfile, "https://other.example/@tim"}, &memUsers{})

	state, _ := setupBody(t, h)
	assert.Len(t, state.Methods, 1)
}

// Once someone owns it, the node stops saying how it is entered.
func TestAClaimedNodeOffersNothing(t *testing.T) {
	store := &memUsers{held: []User{{ID: "US-1", Level: LevelRoot}}}
	h := setupHandler(t, []string{rootProfile}, store)

	state, _ := setupBody(t, h)
	assert.True(t, state.Claimed)
	assert.Empty(t, state.Methods)
}

// A node listing nobody cannot be claimed, and says so rather than looking
// like one that is waiting.
func TestAnUngovernedNodeIsNotWaiting(t *testing.T) {
	h := setupHandler(t, nil, &memUsers{})

	state, _ := setupBody(t, h)
	assert.False(t, state.Governed)
	assert.Empty(t, state.Methods)
}

func claim(h *Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.HandleClaim(rec, httptest.NewRequest(http.MethodPost, "/setup/claim", strings.NewReader(body)))
	return rec
}

// The browser names how, never where. A host it could supply would make this
// an open redirect signed by this node.
func TestAClaimCannotNameAProviderTheNodeDoesNotList(t *testing.T) {
	h := setupHandler(t, []string{rootProfile}, &memUsers{})

	rec := claim(h, `{"provider":"atproto","peer_pubkey_hex":"aa"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// A second claim is not a thing that can happen.
func TestAClaimedNodeRefusesAnotherClaim(t *testing.T) {
	store := &memUsers{held: []User{{ID: "US-1", Level: LevelRoot}}}
	h := setupHandler(t, []string{rootProfile}, store)

	rec := claim(h, `{"provider":"mastodon","peer_pubkey_hex":"aa"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// A store that cannot be read counts as claimed. Refusing to open the door
// beats opening it on a guess.
func TestAnUnreadableStoreIsTreatedAsClaimed(t *testing.T) {
	h := setupHandler(t, []string{rootProfile}, brokenUsers{})

	state, _ := setupBody(t, h)
	assert.True(t, state.Claimed)
	assert.Empty(t, state.Methods)
}
