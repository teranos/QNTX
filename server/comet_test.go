package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/comet"
	"go.uber.org/zap/zaptest"
)

// The route answers through package comet, in the node's own JSON.
func TestTheCometRouteAnswersInTheNodesJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kept := filepath.Join(home, ".qntx", "comets", "abcd-nl", "grove", "x86_64-linux", "ground")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	require.NoError(t, comet.KeepDigest(kept))
	sum := sha256.Sum256([]byte("a comet"))
	s := &QNTXServer{logger: zaptest.NewLogger(t).Sugar()}

	digest := httptest.NewRecorder()
	s.HandleComet(digest, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux?digest", nil))
	assert.Equal(t, http.StatusOK, digest.Code)
	assert.JSONEq(t, `{"digest":"`+hex.EncodeToString(sum[:])+`"}`, digest.Body.String())

	none := httptest.NewRecorder()
	s.HandleComet(none, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/other/x86_64-linux", nil))
	assert.Equal(t, http.StatusNotFound, none.Code)
	assert.Contains(t, none.Body.String(), `"error":"no comet of abcd-nl/other/x86_64-linux is kept`)
}
