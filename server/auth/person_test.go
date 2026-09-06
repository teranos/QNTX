package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asked calls the route behind the gate, the way the table serves it, and hands
// back what came out.
func asked(t *testing.T, h *Handler, req *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.Middleware(everyLevel, h.HandleTheUser)(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	}
	return rec, body
}

// The Self glyph draws the node. This is the person looking at it: the User the
// admission resolved, and not one field about anybody else.
func TestARootSessionIsAnsweredWithItsOwnUser(t *testing.T) {
	h := testHandler()
	store := &memUsers{held: []User{{
		ID:          "US-TIM",
		DisplayName: "tim",
		Level:       LevelRoot,
		Accounts:    []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount, Handle: "@tim@mastodon.example"}},
		Keys:        []UserKey{{DID: "did:key:zBrowser", Origin: OriginBrowser}},
	}}}
	h.users = store

	session, err := h.sessions.create(mastodonAccount, store.held[0])
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec, body := asked(t, h, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "US-TIM", body["user"])
	assert.Equal(t, "tim", body["display_name"])
	assert.Equal(t, "ROOT", body["level"])
	assert.Equal(t, mastodonAccount, body["identity"])
	assert.Equal(t, "session", body["via"])
	// ROOT walked up to no door, so it names none, and no door is every
	// namespace the node serves.
	assert.Nil(t, body["door"])
	assert.Empty(t, body["namespaces"])

	accounts, ok := body["accounts"].([]any)
	require.True(t, ok, "the accounts joined to this User were not drawn")
	require.Len(t, accounts, 1)
	joined, ok := accounts[0].(map[string]any)
	require.True(t, ok, "an account came back as something other than an account")
	assert.Equal(t, "mastodon", joined["provider"])
	assert.Equal(t, mastodonAccount, joined["canonical_id"])

	assert.Equal(t, []any{"did:key:zBrowser"}, body["keys"])
}

// A registration belongs to the door it arrived at, and that door is the
// namespace it acts in (ADR-032). Both are the person's, so both are said.
func TestAPublicRegistrationIsAnsweredWithItsDoorAndNamespace(t *testing.T) {
	h := testHandler()
	h.SetIdentities(nil, nil)
	const arrived = "google:110169484474386276334"
	store := &memUsers{held: []User{{
		ID:        "US-VISITOR",
		Level:     LevelPublicRegistration,
		Namespace: "garden",
		Accounts:  []UserAccount{{Provider: "google", CanonicalID: arrived, Handle: "tim@example.com"}},
	}}}
	h.users = store

	session, err := h.sessions.create(arrived, store.held[0])
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec, body := asked(t, h, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "US-VISITOR", body["user"])
	assert.Equal(t, "PUBLIC_REGISTRATION", body["level"])
	assert.Equal(t, "garden", body["door"])
	assert.Equal(t, []any{"garden"}, body["namespaces"])
	assert.Equal(t, "session", body["via"])
}

// A token speaks for whoever minted it (ADR-025). The answer is that person's
// User, and it says the request came in on a token rather than a session.
func TestATokenIsAnsweredWithTheMintersUserAndSaysSo(t *testing.T) {
	h := testHandler()
	store := &memUsers{held: []User{{
		ID:          "US-TIM",
		DisplayName: "tim",
		Level:       LevelRoot,
		Accounts:    []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}},
	}}}
	h.users = store
	tokens := newMemTokenStore()
	h.tokens = tokens

	raw, _, err := tokens.Create(NewToken{
		Label:        "ci",
		MintedBy:     mastodonAccount,
		MintedByUser: "US-TIM",
		Level:        LevelAttestor,
		Namespaces:   []string{"pond"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec, body := asked(t, h, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "US-TIM", body["user"])
	assert.Equal(t, "token", body["via"])
	assert.Equal(t, "ATTESTOR", body["level"])
	assert.Equal(t, mastodonAccount, body["identity"])
	assert.Equal(t, []any{"pond"}, body["namespaces"])
}

// A blank row would read as a person with nothing about them. The store not
// holding the User the admission named is the node's problem, and it says so.
func TestAUserTheStoreDoesNotHoldIsAnError(t *testing.T) {
	h := testHandler()
	h.users = &memUsers{}

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req = req.WithContext(WithAdmission(req.Context(), Admission{
		level:    LevelRoot,
		Identity: mastodonAccount,
		UserID:   "US-GONE",
	}))

	rec := httptest.NewRecorder()
	h.HandleTheUser(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "US-GONE")
}

// A store that will not answer is not an empty person either.
func TestAStoreThatWillNotAnswerIsSaidRatherThanDrawnBlank(t *testing.T) {
	h := testHandler()
	h.users = brokenUsers{}

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req = req.WithContext(WithAdmission(req.Context(), Admission{
		level:    LevelRoot,
		Identity: mastodonAccount,
		UserID:   "US-TIM",
	}))

	rec := httptest.NewRecorder()
	h.HandleTheUser(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "US-TIM")
}

// Nobody presenting anything gets no answer at all. The gate is what refuses,
// so the handler never has to consider a caller who is not there — and the
// glyph gets the refusal in words rather than the login page.
func TestAStrangerNeverReachesTheirOwnUser(t *testing.T) {
	h := testHandler()
	h.users = &memUsers{}

	req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
	req.Header.Set("Accept", "application/json")
	rec, body := asked(t, h, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "no session", body["error"])
}
