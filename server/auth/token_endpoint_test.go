package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The token endpoint: fosite exchanges the code for the token the strategy
// already mints, and the store writes it with the DID the session carries.

// codeFor runs the whole journey for a minted client and returns the code
// sent to its return address, with the verifier the code was challenged by.
func codeFor(t *testing.T, h *Handler, did string) (code, verifier string) {
	t.Helper()
	verifier, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	next := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)
	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	require.Equal(t, http.StatusSeeOther, done.Code, done.Body.String())
	sent, err := url.Parse(done.Header().Get("Location"))
	require.NoError(t, err)
	code = sent.Query().Get("code")
	require.NotEmpty(t, code)
	return code, verifier
}

// exchangeRequest is the client coming for its token, as RFC 6749 §4.1.3 writes
// it, with its id and secret in Basic auth.
func exchangeRequest(did, secret, code, verifier string) *http.Request {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", appReturn)
	form.Set("code_verifier", verifier)
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(did), url.QueryEscape(secret))
	return r
}

// clientSecret is the raw token the client was minted as: what it presents.
func clientSecret(t *testing.T, store *memTokenStore, did string) string {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, tok := range store.tokens {
		if tok.grant.DID == did {
			// The fake mints qntx_ and the sequence number its id carries, and
			// keeps only the hash, as the real store does.
			n, err := strconv.Atoi(strings.TrimPrefix(tok.id, "AT_"))
			require.NoError(t, err)
			return fmt.Sprintf("qntx_%060d", n)
		}
	}
	t.Fatal("no client in the store")
	return ""
}

// didOf is the did:key the raw token's seed names, derived the way the
// strategy derives it, so the test can check the DID the store wrote is the
// token's own.
func didOf(t *testing.T, raw string) string {
	t.Helper()
	seed, err := hex.DecodeString(strings.TrimPrefix(raw, tokenPrefix))
	require.NoError(t, err)
	return EncodeDIDKey(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
}

// The exchange: the code and the secret go in, and the token QNTX already
// hands out comes back, written in the store with the DID its seed names,
// speaking for the person who said yes, in the namespace the client was
// minted at.
func TestTheCodeIsExchangedForTheTokenTheStoreWrites(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, clientSecret(t, store, did), code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var answer struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Refresh     string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	assert.Equal(t, "bearer", answer.TokenType)
	assert.True(t, strings.HasPrefix(answer.AccessToken, tokenPrefix), answer.AccessToken)
	assert.Greater(t, answer.ExpiresIn, 0)
	// A yes outlasts the hour the access token has: the client comes back with
	// this rather than sending the person to the passkey again.
	assert.NotEmpty(t, answer.Refresh, "no refresh token came back")

	// The store wrote it with the DID the session carries: the token's own.
	grant, ok := store.Lookup(sha256Hex(answer.AccessToken))
	require.True(t, ok, "the issued token does not authenticate")
	assert.Equal(t, didOf(t, answer.AccessToken), grant.DID)
	// "my oauth should hjust have that permission". The level is the
	// person's; the namespace is the client's.
	assert.Equal(t, LevelRoot, grant.Level)
	assert.Equal(t, []string{NamespaceDefault}, grant.Namespaces)
	assert.Equal(t, did, grant.ClientDID)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	// The person is who the identity reaches in the User store (sentHome asks
	// userFor), and this node holds no Users: empty is devoid.
	assert.Empty(t, grant.MintedByUser)

	// Listed under the client's label, with the lifetime the answer named.
	listed, err := store.List()
	require.NoError(t, err)
	var issued *TokenInfo
	for i := range listed {
		if listed[i].DID == grant.DID {
			issued = &listed[i]
		}
	}
	require.NotNil(t, issued)
	assert.Equal(t, "app", issued.Label)
	assert.NotNil(t, issued.ExpiresAt)

	// And it is a bearer the middleware admits as that person: the admission
	// their passkey session gets, not a token's.
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+answer.AccessToken)
	admission, admitted := h.admissionOf(h.presented(req))
	require.True(t, admitted, "the issued token is not admitted as a bearer")
	assert.Equal(t, LevelRoot, admission.level)
	assert.Equal(t, mastodonAccount, admission.Identity)
	assert.Equal(t, []string{NamespaceDefault}, admission.Namespaces)
	assert.Nil(t, admission.Grant, "the token was admitted as a token rather than as the person")
	assert.Equal(t, did, admission.ClientDID, "a connector is not known to be one")
}

// The level is the person's at the moment the token is used, the same question
// a session asks on every request. Struck out of am.toml, the token is nobody.
func TestAPersonsTokenIsNobodyOnceThePersonIs(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, clientSecret(t, store, did), code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))

	h.SetIdentities(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+answer.AccessToken)
	_, admitted := h.admissionOf(h.presented(req))
	assert.False(t, admitted, "the token outlived the person it speaks for")
}

// The secret is the raw token the client was minted as. Anything else is not
// the client, and no token is written.
func TestTheWrongSecretGetsNoToken(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, "qntx_"+strings.Repeat("f", 64), code, verifier))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_client")

	listed, err := store.List()
	require.NoError(t, err)
	assert.Len(t, listed, 1, "a token was written for a stranger")
}

// A client revoked before it came for its token is a door that shut: its
// secret is no longer a live token.
func TestARevokedClientsSecretNoLongerWorks(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)
	listed, err := store.List()
	require.NoError(t, err)
	require.NoError(t, store.Revoke(listed[0].ID))

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, secret, code, verifier))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// PKCE: the verifier has to be the one the challenge was made from.
func TestTheWrongVerifierGetsNoToken(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, _ := codeFor(t, h, did)

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, clientSecret(t, store, did), code, strings.Repeat("x", 43)))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	listed, err := store.List()
	require.NoError(t, err)
	assert.Len(t, listed, 1, "a token was written for a code nobody could verify")
}

// A code is spent once. Spent twice, the second is refused and the token the
// first spend issued is revoked (RFC 6749 §4.1.2).
func TestACodeSpentTwiceRevokesWhatItIssued(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &answer))
	_, live := store.Lookup(sha256Hex(answer.AccessToken))
	require.True(t, live)

	second := httptest.NewRecorder()
	h.handleToken(second, exchangeRequest(did, secret, code, verifier))
	assert.Equal(t, http.StatusBadRequest, second.Code, second.Body.String())
	assert.Contains(t, second.Body.String(), "invalid_grant")

	_, live = store.Lookup(sha256Hex(answer.AccessToken))
	assert.False(t, live, "the token the first spend issued is still live")
}

// refreshRequest is the client coming back with the refresh token rather than
// the person: no passkey, no code, the same secret.
func refreshRequest(did, secret, refresh string) *http.Request {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(did), url.QueryEscape(secret))
	return r
}

// The whole of set-and-forget: an hour later the client comes back with the
// refresh token and gets a new pair, and the person is not asked again. The
// session has to survive the round trip, or the token speaks for nobody.
func TestARefreshTokenGetsANewTokenWithoutThePerson(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		AccessToken string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))
	require.NotEmpty(t, was.Refresh)

	again := httptest.NewRecorder()
	h.handleToken(again, refreshRequest(did, secret, was.Refresh))
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var now struct {
		AccessToken string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &now))

	assert.True(t, strings.HasPrefix(now.AccessToken, tokenPrefix), now.AccessToken)
	assert.NotEqual(t, was.AccessToken, now.AccessToken, "the same access token came back")
	assert.NotEqual(t, was.Refresh, now.Refresh, "the refresh token was not rotated")
	assert.Greater(t, now.ExpiresIn, 0)

	// It speaks for the same person, in the same namespace: what the session
	// carries across the refresh.
	grant, live := store.Lookup(sha256Hex(now.AccessToken))
	require.True(t, live, "the refreshed token does not authenticate")
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	// A refresh reissues through the same store call as the first exchange
	// (fosite flow_refresh.go), so it is still the person.
	assert.Equal(t, LevelRoot, grant.Level)
	assert.Equal(t, []string{NamespaceDefault}, grant.Namespaces)
	assert.Equal(t, did, grant.ClientDID)

	// The one it replaced is spent.
	_, stillLive := store.Lookup(sha256Hex(was.Refresh))
	assert.False(t, stillLive, "the rotated refresh token still works")
}

// A refresh token presented twice is a stolen one as far as the node can
// tell, so what it led to is revoked (RFC 6819 §5.2.2.3).
func TestARefreshTokenSpentTwiceRevokesWhatItIssued(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))

	spent := httptest.NewRecorder()
	h.handleToken(spent, refreshRequest(did, secret, was.Refresh))
	require.Equal(t, http.StatusOK, spent.Code, spent.Body.String())
	var now struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(spent.Body.Bytes(), &now))
	_, live := store.Lookup(sha256Hex(now.AccessToken))
	require.True(t, live)

	twice := httptest.NewRecorder()
	h.handleToken(twice, refreshRequest(did, secret, was.Refresh))
	assert.Equal(t, http.StatusBadRequest, twice.Code, twice.Body.String())
	assert.Contains(t, twice.Body.String(), "invalid_grant")

	// Refusing the second spend is half of it. The node cannot tell the thief
	// from the client, so what the stolen token already bought is taken back
	// as well — otherwise a refresh token lifted once is an hour of access
	// nobody can stop.
	_, stillLive := store.Lookup(sha256Hex(now.AccessToken))
	assert.False(t, stillLive, "the token the reused refresh issued is still live")
}

// A refresh token is written down the way every token is, which is what makes
// it survive a restart — and is why it must not be admitted as a bearer.
func TestARefreshTokenIsNotABearer(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)

	w := httptest.NewRecorder()
	h.handleToken(w, exchangeRequest(did, clientSecret(t, store, did), code, verifier))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answer struct {
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	require.NotEmpty(t, answer.Refresh)

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+answer.Refresh)
	_, admitted := h.admissionOf(h.presented(req))
	assert.False(t, admitted, "a refresh token was admitted as a bearer")
}

// A token acts where its client was minted, not where the person stands.
func TestATokenActsInItsClientsNamespace(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	_, _, err := store.Create(NewToken{
		Label: "vak", MintedBy: mastodonAccount, Level: LevelOAuth,
		Namespaces: []string{"vakconnectie"}, ReturnAddress: appReturn,
	})
	require.NoError(t, err)
	var did string
	listed, err := store.List()
	require.NoError(t, err)
	for _, info := range listed {
		if info.Label == "vak" {
			did = info.DID
		}
	}
	require.NotEmpty(t, did)

	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)
	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		AccessToken string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))
	grant, live := store.Lookup(sha256Hex(was.AccessToken))
	require.True(t, live)
	assert.Equal(t, []string{"vakconnectie"}, grant.Namespaces)

	again := httptest.NewRecorder()
	h.handleToken(again, refreshRequest(did, secret, was.Refresh))
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var now struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &now))
	grant, live = store.Lookup(sha256Hex(now.AccessToken))
	require.True(t, live)
	assert.Equal(t, []string{"vakconnectie"}, grant.Namespaces, "the refresh lost the client's namespace")
}

// restarted is the same node coming back: same DID key, same token store.
func restarted(t *testing.T, before *Handler, store *memTokenStore, key ed25519.PrivateKey) *Handler {
	t.Helper()
	after := handlerWithDoors(t)
	after.configuredOrigin = before.configuredOrigin
	after.tokens = store
	after.nodeKey = key
	return after
}

// A refresh token is written down, and the node that comes back is the same node.
func TestARefreshTokenOutlivesARestart(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))
	require.NotEmpty(t, was.Refresh)

	after := restarted(t, h, store, h.nodeKey)
	again := httptest.NewRecorder()
	after.handleToken(again, refreshRequest(did, secret, was.Refresh))
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var now struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &now))
	grant, live := store.Lookup(sha256Hex(now.AccessToken))
	require.True(t, live, "the token refreshed after the restart does not authenticate")
	assert.Equal(t, mastodonAccount, grant.MintedBy)
}

// Another node holding the same rows is not the node that signed them.
func TestARefreshTokenIsNotAnotherNodesToHonour(t *testing.T) {
	h, store, did := authorizingHandler(t)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))

	other := restarted(t, h, store, testNodeKey(t))
	again := httptest.NewRecorder()
	other.handleToken(again, refreshRequest(did, secret, was.Refresh))
	assert.Equal(t, http.StatusBadRequest, again.Code, again.Body.String())
}

// No DID key is no secret, and no authorize request is served on nothing.
func TestANodeWithoutAKeyServesNoAuthorizeRequest(t *testing.T) {
	h, _, did := authorizingHandler(t)
	h.nodeKey = nil
	_, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// The token endpoint answers a form, and nothing else.
func TestTheTokenEndpointAnswersOnlyAPost(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	w := httptest.NewRecorder()
	h.handleToken(w, httptest.NewRequest(http.MethodGet, tokenPath, nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
