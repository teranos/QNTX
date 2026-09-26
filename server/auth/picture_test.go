package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "qntx should keep the picture"

// signInWithGoogle walks the ceremony to the end: Google answers with this sub
// and this picture, the callback signs, and the browser collects the binding.
func signInWithGoogle(t *testing.T, h *Handler, browser ed25519.PublicKey, door, sub, picture string) SignedBinding {
	t.Helper()
	standInGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sub": sub, "email": "tim@example.com", "name": "Tim", "picture": picture,
		})
	})
	h.SetGoogleClient("client-id", "client-secret")

	const ticket = "the-starting-browser"
	state, err := h.bindingFlows.open(flow{
		provider:      "google",
		peerPubkeyHex: hex.EncodeToString(browser),
		ceremony:      ticket,
		state:         providerState{ClientID: "client-id", ClientSecret: "client-secret"},
		redirectURI:   "https://api.example.com" + callbackPath,
		door:          door,
	})
	require.NoError(t, err)

	back := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+urlEncode(state)+"&code=the-code", nil)
	back.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: ticket})
	w := httptest.NewRecorder()
	h.handleBindingCallback(w, back)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	collect := httptest.NewRequest(http.MethodGet, "/auth/binding/result", nil)
	collect.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: ticket})
	w = httptest.NewRecorder()
	h.handleBindingResult(w, collect)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var result struct {
		Binding SignedBinding `json:"binding"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result.Binding
}

// A door signing somebody up through Google, and the node doing the signing.
func pictureDoor(t *testing.T) (*Handler, *memUsers) {
	t.Helper()
	h, signer, kept := publicDoor(t)
	h.SetNodeKey(signer)
	return h, kept
}

// ROOT through Google, with the device half of the login ready to go.
func pictureRoot(t *testing.T) (*Handler, *memUsers) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	h := handlerWithCreds(t)
	kept := &memUsers{}
	h.users = kept
	h.SetNodeKey(priv)
	h.SetIdentities([]string{"google:110"}, []string{hex.EncodeToString(pub)})
	return h, kept
}

func TestAGoogleRegistrationKeepsThePicture(t *testing.T) {
	h, kept := pictureDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	peer := browser.Public().(ed25519.PublicKey)

	binding := signInWithGoogle(t, h, peer, "garden", "110", "https://lh3.example/tim.jpg")
	w := layeArrives(t, h, browser, []SignedBinding{binding})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, kept.held, 1)
	require.Len(t, kept.held[0].Accounts, 1)
	assert.Equal(t, "https://lh3.example/tim.jpg", kept.held[0].Accounts[0].Picture)
	assert.Equal(t, "https://lh3.example/tim.jpg", kept.held[0].Picture())
}

func TestARootLoginThroughGoogleKeepsThePicture(t *testing.T) {
	h, kept := pictureRoot(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	peer := browser.Public().(ed25519.PublicKey)

	binding := signInWithGoogle(t, h, peer, "", "110", "https://lh3.example/tim.jpg")
	w := layeArrives(t, h, browser, []SignedBinding{binding})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, kept.held, 1)
	require.Len(t, kept.held[0].Accounts, 1)
	assert.Equal(t, "https://lh3.example/tim.jpg", kept.held[0].Accounts[0].Picture)
}

// The provider's picture now is the picture, whatever it was last time.
func TestANewerPictureReplacesTheOlder(t *testing.T) {
	h, kept := pictureRoot(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	peer := browser.Public().(ed25519.PublicKey)

	for _, picture := range []string{"https://lh3.example/old.jpg", "https://lh3.example/new.jpg"} {
		binding := signInWithGoogle(t, h, peer, "", "110", picture)
		w := layeArrives(t, h, browser, []SignedBinding{binding})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	require.Len(t, kept.held, 1)
	require.Len(t, kept.held[0].Accounts, 1, "logging in again added a second account")
	assert.Equal(t, "https://lh3.example/new.jpg", kept.held[0].Accounts[0].Picture)
}

func TestANewerPictureReplacesTheOlderAtADoor(t *testing.T) {
	h, kept := pictureDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	peer := browser.Public().(ed25519.PublicKey)

	for _, picture := range []string{"https://lh3.example/old.jpg", "https://lh3.example/new.jpg"} {
		binding := signInWithGoogle(t, h, peer, "garden", "110", picture)
		w := layeArrives(t, h, browser, []SignedBinding{binding})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	require.Len(t, kept.held, 1)
	assert.Equal(t, "https://lh3.example/new.jpg", kept.held[0].Picture())
}

// A door that lost its localStorage asks the node who is signed in, and the
// picture comes back with the answer.
func TestStatusCarriesThePicture(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{"google:110"}, nil)
	kept := &memUsers{held: []User{{
		ID:       "US-TIM",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110", Picture: "https://lh3.example/tim.jpg"}},
	}}}
	h.users = kept

	session, err := h.sessions.create("google:110", kept.held[0])
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	w := httptest.NewRecorder()
	h.handleStatus(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var said map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &said))
	assert.Equal(t, "https://lh3.example/tim.jpg", said["picture"])
}

func TestTheUserCarriesThePicture(t *testing.T) {
	h := testHandler()
	h.SetIdentities([]string{"google:110"}, nil)
	kept := &memUsers{held: []User{{
		ID:       "US-TIM",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110", Picture: "https://lh3.example/tim.jpg"}},
	}}}
	h.users = kept

	session, err := h.sessions.create("google:110", kept.held[0])
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/i/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec, body := asked(t, h, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://lh3.example/tim.jpg", body["picture"])
}

// The way home ends at the door with a session, and the picture rides with it.
func TestTheWayHomeCarriesThePicture(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.SetIdentities([]string{"google:110"}, nil)
	h.users = &memUsers{held: []User{{
		ID:       "US-TIM",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110", Picture: "https://lh3.example/tim.jpg"}},
	}}}
	h.homewards.Store("the-ticket", homeward{door: "https://portal.garden.test", startedAt: time.Now()})

	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: "the-ticket"})
	h.sentHome(httptest.NewRecorder(), finish, "s3ss", "google:110")

	r := httptest.NewRequest(http.MethodGet, homewardResultPath+"?home=the-ticket", nil)
	r.Header.Set("Origin", "https://portal.garden.test")
	w := httptest.NewRecorder()
	h.handleHomewardResult(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var said map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &said))
	assert.Equal(t, "https://lh3.example/tim.jpg", said["picture"])
}
