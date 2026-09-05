package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two sides of an Apple ceremony that hold keys: the operator, whose .p8
// signs the client secret, and Apple, whose RSA key signs the identity token.
type appleKeys struct {
	operator    *ecdsa.PrivateKey
	operatorPEM string
	apple       *rsa.PrivateKey
	kid         string
}

func newAppleKeys(t *testing.T) appleKeys {
	t.Helper()
	operator, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(operator)
	require.NoError(t, err)
	apple, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return appleKeys{
		operator:    operator,
		operatorPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		apple:       apple,
		kid:         "nP41CGTvOz",
	}
}

// The client an operator would hold after the developer portal: a Services ID,
// a Team ID, a Key ID and the key that portal handed out.
func (k appleKeys) client() OperatorClient {
	return OperatorClient{
		ID:     "com.example.qntx.web",
		Secret: k.operatorPEM,
		TeamID: "DEF123GHIJ",
		KeyID:  "ABC123DEFG",
	}
}

func (k appleKeys) state(nonce string) providerState {
	c := k.client()
	return providerState{
		Host:         appleAuthHost,
		ClientID:     c.ID,
		ClientSecret: c.Secret,
		TeamID:       c.TeamID,
		KeyID:        c.KeyID,
		Nonce:        nonce,
	}
}

// idToken is what Apple's token endpoint says about the person, signed by the
// key its JWKS publishes. Callers override claims to make it lie.
func (k appleKeys) idToken(t *testing.T, signer *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	all := jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"aud":   "com.example.qntx.web",
		"sub":   "001234.abcd1234abcd1234abcd1234abcd1234.5678",
		"email": "someone@privaterelay.appleid.com",
		"exp":   time.Now().Add(10 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": "the-nonce",
	}
	for name, value := range claims {
		if value == nil {
			delete(all, name)
			continue
		}
		all[name] = value
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, all)
	token.Header["kid"] = k.kid
	signed, err := token.SignedString(signer)
	require.NoError(t, err)
	return signed
}

func (k appleKeys) jwks() map[string]any {
	pub := k.apple.PublicKey
	return map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": k.kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
}

// standInApple points the exchange at a server this test controls, and puts
// the real endpoints back afterwards.
func standInApple(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	tokenWas, keysWas := appleTokenURL, appleKeysURL
	appleTokenURL = server.URL + "/auth/token"
	appleKeysURL = server.URL + "/auth/keys"
	t.Cleanup(func() {
		appleTokenURL, appleKeysURL = tokenWas, keysWas
		server.Close()
	})
}

// fakeApple answers the token endpoint with idToken and the keys endpoint
// with the JWKS, and records the client secret the exchange sent.
func fakeApple(t *testing.T, k appleKeys, idToken string) *string {
	t.Helper()
	var secret string
	standInApple(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "com.example.qntx.web", r.PostForm.Get("client_id"))
			assert.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
			assert.Equal(t, "the-code", r.PostForm.Get("code"))
			assert.Equal(t, "https://api.example.com/auth/binding/callback", r.PostForm.Get("redirect_uri"))
			secret = r.PostForm.Get("client_secret")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "spent-once",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     idToken,
			})
		case "/auth/keys":
			_ = json.NewEncoder(w).Encode(k.jwks())
		default:
			t.Errorf("the exchange asked for %s, which Apple does not serve", r.URL.Path)
		}
	})
	return &secret
}

// The consent URL carries the operator's Services ID and this node's
// redirect, asks Apple to return by POST, and carries nothing the browser
// wrote. The key goes into the state, never into the URL.
func TestAppleAuthorizeCarriesTheOperatorsClient(t *testing.T) {
	k := newAppleKeys(t)
	p := appleProvider(k.client())

	authorize, st, err := p.authorize(context.Background(), appleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(authorize, "https://appleid.apple.com/auth/authorize?"), authorize)
	assert.Contains(t, authorize, "client_id=com.example.qntx.web")
	assert.Contains(t, authorize, "redirect_uri=https%3A%2F%2Fapi.example.com%2Fauth%2Fbinding%2Fcallback")
	assert.Contains(t, authorize, "response_type=code")
	assert.Contains(t, authorize, "response_mode=form_post")
	assert.Contains(t, authorize, "scope=name+email")
	assert.Contains(t, authorize, "nonce="+st.Nonce)
	assert.NotEmpty(t, st.Nonce)
	assert.NotContains(t, authorize, "PRIVATE KEY")

	assert.Equal(t, k.operatorPEM, st.ClientSecret)
	assert.Equal(t, "com.example.qntx.web", st.ClientID)
	assert.Equal(t, "DEF123GHIJ", st.TeamID)
	assert.Equal(t, "ABC123DEFG", st.KeyID)
}

// Two ceremonies are two nonces, so an identity token minted for one cannot
// finish the other.
func TestAppleMintsANonceForEachCeremony(t *testing.T) {
	p := appleProvider(newAppleKeys(t).client())
	_, first, err := p.authorize(context.Background(), appleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)
	_, second, err := p.authorize(context.Background(), appleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)
	assert.NotEqual(t, first.Nonce, second.Nonce)
}

// The client secret is not a string the operator holds: it is a JWT the node
// signs with the operator's key, naming the team, the key and the client, and
// it lives minutes. sub is the identity, qualified so am.toml says what it is;
// the email is a handle Apple may have relayed.
func TestAppleExchangeReturnsAQualifiedSub(t *testing.T) {
	k := newAppleKeys(t)
	secret := fakeApple(t, k, k.idToken(t, k.apple, nil))

	acct, err := appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.Equal(t, "apple:001234.abcd1234abcd1234abcd1234abcd1234.5678", acct.CanonicalID)
	assert.Equal(t, "someone@privaterelay.appleid.com", acct.Handle)

	// What the token endpoint was handed as the secret.
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(*secret, claims, func(token *jwt.Token) (any, error) {
		return &k.operator.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired())
	require.NoError(t, err, "the client secret is not a JWT the operator's key signed")
	assert.Equal(t, "ABC123DEFG", parsed.Header["kid"])
	assert.Equal(t, "DEF123GHIJ", claims["iss"])
	assert.Equal(t, "com.example.qntx.web", claims["sub"])
	assert.Equal(t, "https://appleid.apple.com", claims["aud"])
	exp, err := claims.GetExpirationTime()
	require.NoError(t, err)
	assert.Less(t, time.Until(exp.Time), 15*time.Minute, "a secret minted for one exchange outlives the exchange by a lot")
}

// An identity token minted for some other ceremony carries some other nonce.
// Believing it would let a token obtained anywhere finish a ceremony here.
func TestAppleExchangeRefusesAnIdTokenForAnotherCeremony(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, jwt.MapClaims{"nonce": "some-other-ceremony"}))

	_, err := appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonce")
}

// The signature is checked against the keys Apple publishes, by kid. A token
// signed by anyone else, under Apple's kid, is refused.
func TestAppleExchangeRefusesAnIdTokenAStrangerSigned(t *testing.T) {
	k := newAppleKeys(t)
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	fakeApple(t, k, k.idToken(t, stranger, nil))

	_, err = appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity token")
}

// A token Apple issued to some other client is not about this node's ceremony.
func TestAppleExchangeRefusesAnIdTokenForAnotherClient(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, jwt.MapClaims{"aud": "com.someone.else"}))

	_, err := appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
}

// An expired token is one Apple no longer stands behind.
func TestAppleExchangeRefusesAnExpiredIdToken(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}))

	_, err := appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
}

// Without a sub there is nothing to match against auth.root_identities, and
// an account keyed on a relayed email is a door that changes hands.
func TestAppleExchangeRefusesAnIdTokenWithNoSub(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, jwt.MapClaims{"sub": nil}))

	_, err := appleExchange(context.Background(), k.state("the-nonce"), "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sub")
}

// A key that is not an EC key cannot sign ES256, and the failure names the
// key rather than showing it.
func TestAppleExchangeRefusesAKeyThatCannotSign(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, nil))
	st := k.state("the-nonce")
	st.ClientSecret = "not a pem at all"

	_, err := appleExchange(context.Background(), st, "the-code",
		"https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ABC123DEFG")
	assert.NotContains(t, err.Error(), "not a pem at all")
}

// An Apple button on a node holding no key is a button that can only fail.
// Half a client — a Services ID with no team, no key id, or no key — is the
// same button.
func TestAppleIsOfferedOnlyOnceConfigured(t *testing.T) {
	h := &Handler{logger: testLogger()}
	_, known := h.providerAt(NamespaceDefault, "apple")
	assert.False(t, known, "apple before it is configured")

	h.SetAppleClient("com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", "-----BEGIN PRIVATE KEY-----")
	p, known := h.providerAt(NamespaceDefault, "apple")
	require.True(t, known, "apple once configured")
	assert.Equal(t, kindRedirect, p.Kind)
	assert.Empty(t, p.HostPrompt, "Apple's ceremony is at one place; nobody is asked where")
	assert.Equal(t, appleAuthHost, p.HostDefault)
	assert.Equal(t, appleAuthHost, hostFor(p, "evil.example.com"))

	for what, halves := range map[string][4]string{
		"no team":   {"com.example.qntx.web", "", "ABC123DEFG", "key"},
		"no key id": {"com.example.qntx.web", "DEF123GHIJ", "", "key"},
		"no key":    {"com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", ""},
		"no client": {"", "DEF123GHIJ", "ABC123DEFG", "key"},
	} {
		h.SetAppleClient(halves[0], halves[1], halves[2], halves[3])
		_, known = h.providerAt(NamespaceDefault, "apple")
		assert.False(t, known, "apple with %s", what)
	}
}

// Google and Apple are offered side by side, and neither grows a copy of the
// other into the shared list.
func TestAppleAndGoogleAreOfferedTogether(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetGoogleClient("client-id", "client-secret")
	h.SetAppleClient("com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", "key")

	offered := h.offeredAt(NamespaceDefault)
	ids := make([]string, 0, len(offered))
	for _, p := range offered {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"mastodon", "atproto", "google", "apple"}, ids)
	assert.Len(t, providers, 2, "the shared list grew")
}

// A door brings its own Apple client the way it brings its own Google one.
func TestADoorConsentsUnderItsOwnAppleClient(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetAppleClient("com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", "the-nodes-key")
	h.doors.set(map[string]*door{
		"https://garden.example": {
			namespace: "garden",
			clients: map[string]OperatorClient{
				"apple": {ID: "garden.services.id", Secret: "gardens-key", TeamID: "GARDEN1234", KeyID: "GARDENKEY1"},
			},
		},
		"https://pond.example": {namespace: "pond"},
	})

	p, ok := h.providerAt("garden", "apple")
	require.True(t, ok)
	authorize, st, err := p.authorize(context.Background(), appleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)
	assert.Contains(t, authorize, "client_id=garden.services.id")
	assert.Equal(t, "GARDEN1234", st.TeamID)
	assert.Equal(t, "gardens-key", st.ClientSecret)

	p, ok = h.providerAt("pond", "apple")
	require.True(t, ok)
	authorize, _, err = p.authorize(context.Background(), appleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)
	assert.Contains(t, authorize, "client_id=com.example.qntx.web")
}

// An apple: entry in auth.root_identities is a way in the setup can offer as a
// single press, the way google: is.
func TestAnAppleEntryIsClaimable(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetAppleClient("com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", "key")

	id, ok := h.claimable("apple:001234.abcd")
	require.True(t, ok)
	assert.Equal(t, "apple", id.provider)
	assert.Equal(t, appleAuthHost, id.host)
}

// Apple returns by POST. A browser keeps a SameSite=Lax cookie back from a
// cross-site POST, and the ceremony stands on that cookie — so the POST is
// answered with a redirect to the same callback as a GET, which the cookie
// rides, and every check happens there.
func TestApplesReturnByPostBecomesTheGetTheCookieRides(t *testing.T) {
	h := ticketHandler()
	state, err := h.bindingFlows.open(flow{provider: "apple", ceremony: "the-starting-browser"})
	require.NoError(t, err)

	form := url.Values{
		"state": {state},
		"code":  {"the-code"},
		"user":  {`{"name":{"firstName":"Ada","lastName":"Lovelace"},"email":"ada@example.com"}`},
	}
	req := httptest.NewRequest(http.MethodPost, callbackPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://appleid.apple.com")
	// No ceremony cookie: the browser did not send it on a cross-site POST.

	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, req)

	require.Equal(t, http.StatusSeeOther, recorded.Code, recorded.Body.String())
	location, err := url.Parse(recorded.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, callbackPath, location.Path)
	assert.Equal(t, state, location.Query().Get("state"))
	assert.Equal(t, "the-code", location.Query().Get("code"))
	assert.NotContains(t, recorded.Header().Get("Location"), "Lovelace", "the name went into the URL")

	// The POST decided nothing: the ceremony is still open, and it now knows
	// what the person is called.
	fl, open := h.bindingFlows.close(state)
	require.True(t, open, "the POST consumed the ceremony")
	assert.Equal(t, "Ada Lovelace", fl.name)
}

// The name Apple sends once rides beside the binding the way Google's does,
// and the GET after the POST is held to the ticket exactly as Google's is.
func TestTheNameApplePostsRidesBesideTheBinding(t *testing.T) {
	k := newAppleKeys(t)
	fakeApple(t, k, k.idToken(t, k.apple, nil))

	h := ticketHandler()
	h.nodeKey = testNodeKey(t)
	h.SetAppleClient("com.example.qntx.web", "DEF123GHIJ", "ABC123DEFG", k.operatorPEM)
	state, err := h.bindingFlows.open(flow{
		provider:    "apple",
		ceremony:    "the-starting-browser",
		state:       k.state("the-nonce"),
		redirectURI: "https://api.example.com/auth/binding/callback",
		door:        NamespaceDefault,
	})
	require.NoError(t, err)

	form := url.Values{
		"state": {state},
		"code":  {"the-code"},
		"user":  {`{"name":{"firstName":"Ada","lastName":"Lovelace"}}`},
	}
	post := httptest.NewRequest(http.MethodPost, callbackPath, strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, post)
	require.Equal(t, http.StatusSeeOther, recorded.Code)

	// A browser that did not start the ceremony follows the redirect: refused.
	stranger := httptest.NewRequest(http.MethodGet, recorded.Header().Get("Location"), nil)
	recorded = httptest.NewRecorder()
	h.handleBindingCallback(recorded, stranger)
	assert.Equal(t, http.StatusForbidden, recorded.Code)

	// The ceremony is spent either way; the starting browser starts again.
	state, err = h.bindingFlows.open(flow{
		provider:    "apple",
		ceremony:    "the-starting-browser",
		state:       k.state("the-nonce"),
		redirectURI: "https://api.example.com/auth/binding/callback",
		door:        NamespaceDefault,
		name:        "Ada Lovelace",
	})
	require.NoError(t, err)
	get := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+state+"&code=the-code", nil)
	get.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "the-starting-browser"})
	recorded = httptest.NewRecorder()
	h.handleBindingCallback(recorded, get)
	require.Equal(t, http.StatusOK, recorded.Code, recorded.Body.String())
	assert.Contains(t, recorded.Body.String(), "Linked as someone@privaterelay.appleid.com")

	val, ok := h.signedBindings.Load("the-starting-browser")
	require.True(t, ok)
	held := val.(heldBinding)
	assert.Equal(t, "apple:001234.abcd1234abcd1234abcd1234abcd1234.5678", held.binding.Claim.CanonicalID)
	assert.Equal(t, "Ada Lovelace", held.name)
}

// A refusal arrives by POST too, and is said on the GET like Google's is.
func TestApplesRefusalByPostIsSaidOnTheGet(t *testing.T) {
	h := ticketHandler()
	state, err := h.bindingFlows.open(flow{provider: "apple", ceremony: "the-starting-browser"})
	require.NoError(t, err)

	form := url.Values{"state": {state}, "error": {"user_cancelled_authorize"}}
	req := httptest.NewRequest(http.MethodPost, callbackPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, req)

	require.Equal(t, http.StatusSeeOther, recorded.Code)
	location, err := url.Parse(recorded.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "user_cancelled_authorize", location.Query().Get("error"))

	get := httptest.NewRequest(http.MethodGet, recorded.Header().Get("Location"), nil)
	recorded = httptest.NewRecorder()
	h.handleBindingCallback(recorded, get)
	assert.Contains(t, recorded.Body.String(), "Authorization was refused: user_cancelled_authorize")
}

// A POST naming no ceremony has nowhere to be redirected to.
func TestAPostNamingNoCeremonyIsRefused(t *testing.T) {
	h := ticketHandler()
	req := httptest.NewRequest(http.MethodPost, callbackPath, strings.NewReader("code=the-code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, req)
	assert.Equal(t, http.StatusBadRequest, recorded.Code)
}

// The name is the person's own typing, passed straight through from the
// browser. It is read for what it is and bounded; a name that is not JSON is
// no name.
func TestAppleNameIsReadAndBounded(t *testing.T) {
	assert.Equal(t, "Ada Lovelace", appleName(`{"name":{"firstName":"Ada","lastName":"Lovelace"}}`))
	assert.Equal(t, "Ada", appleName(`{"name":{"firstName":"  Ada  "}}`))
	assert.Equal(t, "", appleName(`{"email":"ada@example.com"}`))
	assert.Equal(t, "", appleName(`not json`))
	assert.Equal(t, "", appleName(""))
	long := strings.Repeat("a", 500)
	assert.Len(t, appleName(`{"name":{"firstName":"`+long+`"}}`), maxAppleNameBytes)
}
