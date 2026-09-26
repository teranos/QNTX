package auth

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askedForThePicture serves /i/picture through the gate, the way the mux does.
func askedForThePicture(h *Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Middleware("/i/picture", everyLevel, h.HandlePicture)(rec, req)
	return rec
}

// The browser asks the node for the picture, and the node asks the provider.
func TestThePictureIsAnsweredAsTheNodesOwn(t *testing.T) {
	var asked atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG-BYTES"))
	}))
	defer provider.Close()
	was := pictureClient
	pictureClient = provider.Client
	defer func() { pictureClient = was }()

	h := testHandler()
	h.SetIdentities([]string{"google:110"}, nil)
	kept := &memUsers{held: []User{{
		ID:       "US-TIM",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110", Picture: provider.URL + "/tim.jpg"}},
	}}}
	h.users = kept

	session, err := h.sessions.create("google:110", kept.held[0])
	require.NoError(t, err)

	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/i/picture", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		rec := askedForThePicture(h, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
		assert.Equal(t, "PNG-BYTES", rec.Body.String())
	}
	// Once from the provider; the second answer is from memory.
	assert.Equal(t, int32(1), asked.Load())
}

func TestAPersonNoProviderShowedHasNoPicture(t *testing.T) {
	h := testHandler()
	h.SetIdentities([]string{"google:110"}, nil)
	kept := &memUsers{held: []User{{
		ID:       "US-TIM",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110"}},
	}}}
	h.users = kept

	session, err := h.sessions.create("google:110", kept.held[0])
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/i/picture", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := askedForThePicture(h, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAPictureThatIsNotHTTPSIsRefused(t *testing.T) {
	_, err := fetchPicture("http://lh3.example/tim.jpg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an https URL")
}
