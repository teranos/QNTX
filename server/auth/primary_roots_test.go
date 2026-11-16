package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "the root identities administer the laye or mycelia network, however we want to call it"

// laye answering who the roots are, to a node that signs as itself.
func layeSaying(t *testing.T, roots []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rootsPath {
			http.NotFound(w, r)
			return
		}
		require.NoError(t, r.ParseForm())
		if r.PostForm.Get("client_assertion_type") != clientAssertionType || r.PostForm.Get("client_assertion") == "" {
			http.Error(w, "unsigned", http.StatusUnauthorized)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string][]string{"root_identities": roots}))
	}))
}

func nodeWithPrimary(t *testing.T, primary string) *Handler {
	t.Helper()
	h := &Handler{logger: testLogger()}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	h.SetNodeKey(key)
	require.NoError(t, h.SetPrimary(primary))
	return h
}

func TestANodeWhosePrimaryIsLayeGoesByLayesRoots(t *testing.T) {
	laye := layeSaying(t, []string{"github:1739143"})
	defer laye.Close()
	h := nodeWithPrimary(t, laye.URL)
	h.SetIdentities(nil, nil)

	require.NoError(t, h.RefreshPrimaryRoots(time.Now()))
	assert.Equal(t, []string{"github:1739143"}, h.identities.roots())
	assert.Equal(t, LevelRoot, h.levelOf("github:1739143"))
}

func TestRootsLastHeardStandWhenThePrimaryDoesNotAnswer(t *testing.T) {
	laye := layeSaying(t, []string{"github:1739143"})
	h := nodeWithPrimary(t, laye.URL)
	require.NoError(t, h.RefreshPrimaryRoots(time.Now()))
	laye.Close()

	assert.Error(t, h.RefreshPrimaryRoots(time.Now()))
	assert.Equal(t, []string{"github:1739143"}, h.identities.roots())
}

func TestAPrimaryThatIsNotLayeHasNoRootsToSay(t *testing.T) {
	notLaye := httptest.NewServer(http.NotFoundHandler())
	defer notLaye.Close()
	h := nodeWithPrimary(t, notLaye.URL)
	h.SetIdentities([]string{"did:key:z6Mkexample"}, nil)

	require.NoError(t, h.RefreshPrimaryRoots(time.Now()))
	assert.Equal(t, []string{"did:key:z6Mkexample"}, h.identities.roots())
}

func TestANodeWithNoPrimaryAsksNobody(t *testing.T) {
	h := &Handler{logger: testLogger()}
	assert.NoError(t, h.RefreshPrimaryRoots(time.Now()))
}
