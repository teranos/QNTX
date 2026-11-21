package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「[Fable 5.2] Where the primary reaches k, which no test can: a file that arrived went another way.」
const whereThePrimaryReachesK = "http://10.88.0.2:8770"

// "to me it feels like k should own my Media Assets"
// "just for clarification, the bytes dont go through Q to K but L to K right?"
func TestAPeerBringsAFileStraightToAPeer(t *testing.T) {
	primary, l := primaryAskingGitHub(t, asksGitHubForNobody, locatesGitHubForNobody)
	lDID, err := l.ownDID()
	require.NoError(t, err)

	// k: a peer of the same primary, keeping what it is brought at an address of its own.
	kKey, kDID := otherNode(t)
	k := handlerWithDoors(t)
	k.nodeKey = kKey
	require.NoError(t, k.SetPrimary(primary.configuredOrigin))
	var brought []byte
	var by Grant
	kServed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files" {
			http.Error(w, "k keeps a file at /api/files, and this asked "+r.URL.Path, http.StatusNotFound)
			return
		}
		signed, carries := signedAssertion(r)
		if !carries {
			http.Error(w, "nothing signed came with the file", http.StatusUnauthorized)
			return
		}
		grant, err := k.signedGrant(signed, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		brought, by = content, grant
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"there","asset":"sha256:abc"}`))
	}))
	t.Cleanup(kServed.Close)
	require.NoError(t, primary.DeclarePeer("k", kDID, []string{"Ground"}, whereThePrimaryReachesK, whereThePrimaryReachesK))
	require.NoError(t, l.SetDirect(map[string]string{"k": kServed.URL}))

	status, said, err := l.BringFileToPeer(context.Background(), "k", "shot.jpeg", []byte("the picture"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status, string(said))
	assert.JSONEq(t, `{"id":"there","asset":"sha256:abc"}`, string(said))
	assert.Equal(t, "the picture", string(brought))

	// k was told a peer brought it, and which: no person, and no level a person holds.
	assert.Equal(t, LevelPeer, by.Level)
	assert.Equal(t, lDID, by.MintedBy)
}

// A peer the primary does not know is brought nothing, and the node is told so.
func TestAFileIsBroughtOnlyToAPeerThePrimaryKnows(t *testing.T) {
	_, l := primaryAskingGitHub(t, asksGitHubForNobody, locatesGitHubForNobody)
	_, _, err := l.BringFileToPeer(context.Background(), "nobody", "shot.jpeg", []byte("the picture"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no peer is called nobody")
}

// A stranger asking to be vouched for, carrying no node's assertion, is refused.
func TestOnlyANodeIsVouchedForToAPeer(t *testing.T) {
	primary, _ := primaryAskingGitHub(t, asksGitHubForNobody, locatesGitHubForNobody)
	form := url.Values{"to": {peerName}}
	r := httptest.NewRequest(http.MethodPost, filesForPeersPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	primary.handleFilesForPeers(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// A node with no primary knows no peer.
func TestANodeWithNoPrimaryBringsAFileNowhere(t *testing.T) {
	_, _, err := handlerWithDoors(t).BringFileToPeer(context.Background(), "k", "shot.jpeg", []byte("the picture"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no primary")
}

// Where a node reaches a peer itself is an origin, or the node does not start on it.
func TestWhereAPeerIsReachedIsAnOrigin(t *testing.T) {
	h := handlerWithDoors(t)
	require.NoError(t, h.SetDirect(map[string]string{"k": "http://k.example.net:8770/"}))
	assert.Equal(t, "http://k.example.net:8770", h.direct["k"])
	require.Error(t, h.SetDirect(map[string]string{"k": "k.example.net"}))
}
