package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/comet"
)

// "cant k use piggyback on what q offers"
//
// A comet on a peer asks GitHub through the primary: where the branch is, and
// where its archive is kept, which the peer fetches.
func TestAPeersCometAsksGitHubThroughItsPrimary(t *testing.T) {
	archive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the archive"))
	}))
	t.Cleanup(archive.Close)
	var asked []string
	s := &QNTXServer{primaryGitHub: primaryGitHub{
		get: func(_ context.Context, url string) ([]byte, error) {
			asked = append(asked, url)
			return []byte(`[{"sha":"3a65c998"}]`), nil
		},
		locate: func(_ context.Context, url string) (string, error) {
			asked = append(asked, url)
			return archive.URL + "/legacy.tar.gz/3a65c998?token=signed", nil
		},
		held: true,
	}}
	ground := comet.Source{Owner: "teranos", Repo: "ground", Branch: "main"}

	rev, err := cometNode{s}.Latest(context.Background(), ground)
	require.NoError(t, err)
	assert.Equal(t, "3a65c998", rev)

	tarball := filepath.Join(t.TempDir(), "ground.tar.gz")
	require.NoError(t, cometNode{s}.Fetch(context.Background(), ground, rev, tarball))
	fetched, err := os.ReadFile(tarball)
	require.NoError(t, err)
	assert.Equal(t, "the archive", string(fetched))
	assert.Equal(t, []string{
		"https://api.github.com/repos/teranos/ground/commits?per_page=1&sha=main",
		"https://api.github.com/repos/teranos/ground/tarball/3a65c998",
	}, asked)
}
