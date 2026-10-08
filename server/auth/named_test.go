package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "There is one ROOT User. A SUPER User is created by it and by nobody else."
// (ADR-031)

func creating(h *Handler, session, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/users", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.sessionOnly(h.usersCollection)(rec, req)
	return rec
}

// ROOT names a person by the account they will prove, and a User exists for
// it: SUPER, made by ROOT, holding that account and nothing more yet.
func TestRootNamesAPerson(t *testing.T) {
	h, store, rootSession := switchingHandler(t)
	root := store.held[0]

	rec := creating(h, rootSession, `{"account":"google:110","display_name":"Ada"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var made User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &made))
	assert.Equal(t, LevelSuper, made.Level)
	assert.Equal(t, root.ID, made.CreatedBy)
	assert.Equal(t, "Ada", made.DisplayName)
	require.Len(t, made.Accounts, 1)
	assert.Equal(t, "google", made.Accounts[0].Provider)
	assert.Equal(t, "google:110", made.Accounts[0].CanonicalID)
	assert.Nil(t, made.Accounts[0].Binding, "nothing has been proven yet")

	require.Len(t, store.held, 2)
	assert.Equal(t, made.ID, store.held[1].ID)
	assert.Equal(t, LevelSuper, h.levelOf("google:110"), "the account is admitted as the person named")
}

// Only ROOT names a person: a person ROOT named, SUPER, names nobody.
func TestOnlyRootNamesAPerson(t *testing.T) {
	h, store, rootSession := switchingHandler(t)
	require.Equal(t, http.StatusCreated, creating(h, rootSession, `{"account":"google:110"}`).Code)
	named := store.held[1]
	theirSession, err := h.sessions.create("google:110", named)
	require.NoError(t, err)
	rec := creating(h, theirSession, `{"account":"google:111"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Len(t, store.held, 2)
}

// An account is one as auth.root_identities names one, and reaches nobody yet:
// not ROOT, and not a User already there.
func TestAnAccountNamedIsOneNobodyReaches(t *testing.T) {
	h, _, rootSession := switchingHandler(t)
	for body, want := range map[string]int{
		`{"account":""}`:                                http.StatusBadRequest,
		`{"account":"110"}`:                             http.StatusBadRequest,
		`{"account":"google:"}`:                         http.StatusBadRequest,
		`{"account":"` + mastodonAccount + `"}`:         http.StatusConflict,
		`{"account":"google:110"}`:                      http.StatusCreated,
		`{"account":" google:110 "}`:                    http.StatusConflict,
		`{"account":"did:plc:abc"}`:                     http.StatusCreated,
		`{"account":"https://m.example/@ada"}`:          http.StatusCreated,
		`{"account":"apple:xyz","display_name":"root"}`: http.StatusBadRequest,
	} {
		rec := creating(h, rootSession, body)
		assert.Equal(t, want, rec.Code, "%s: %s", body, rec.Body.String())
	}
}

// The account named is admitted at login the way a listed one is: the
// provider vouches for it, and the node admits them as the User ROOT made,
// as themselves. A key alone names nobody.
func TestAPersonNamedIsAdmittedAsThemselves(t *testing.T) {
	h, store, rootSession := switchingHandler(t)
	signerPub, signer, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	h.SetIdentities([]string{mastodonAccount}, []string{hex.EncodeToString(signerPub)})
	require.Equal(t, http.StatusCreated, creating(h, rootSession, `{"account":"google:110"}`).Code)
	named := store.held[1]

	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	browserDID := EncodeDIDKey(browser.Public().(ed25519.PublicKey))
	vouched := vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@example")

	admitted, matched, ok := h.admits(browserDID, []SignedBinding{vouched})
	require.True(t, ok, "the account ROOT named was not admitted")
	assert.Equal(t, "google:110", admitted)
	require.NotNil(t, matched)

	_, _, ok = h.admits(browserDID, nil)
	assert.False(t, ok, "a key alone was admitted")
	_, _, ok = h.admits(browserDID, []SignedBinding{vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:999", "")})
	assert.False(t, ok, "an account nobody named was admitted")

	// Logging in joins the browser's key to the User ROOT made, and keeps what
	// the provider said for the account, which was written before it spoke.
	u, err := h.joinUser(admitted, matched, browserDID)
	require.NoError(t, err)
	assert.Equal(t, named.ID, u.ID, "a second User was made for a person ROOT already named")
	assert.True(t, u.HoldsKey(browserDID))
	require.Len(t, u.Accounts, 1)
	assert.Equal(t, "ada@example", u.Accounts[0].Handle)
	require.NotNil(t, u.Accounts[0].Binding)
	assert.Len(t, store.held, 2)

	// Their session is SUPER, read off the User and asserted nowhere.
	session, err := h.sessions.create(admitted, u)
	require.NoError(t, err)
	var seen Admission
	guarded := h.Middleware("/test", everyLevel, func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
	})
	r := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	guarded(httptest.NewRecorder(), r)
	assert.Equal(t, string(LevelSuper), seen.LevelName())
	assert.Equal(t, named.ID, seen.UserID)
}

// ROOT switching the person off is the whole of taking them back: the User
// stands, switched off, and the account no longer admits.
func TestAPersonNamedIsSwitchedOffByRoot(t *testing.T) {
	h, store, rootSession := switchingHandler(t)
	require.Equal(t, http.StatusCreated, creating(h, rootSession, `{"account":"google:110"}`).Code)
	named := store.held[1]
	rec := asRoot(h, rootSession, http.MethodPost, "/auth/users/"+named.ID+"/disable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, store.held[1].SwitchedOff())
}
