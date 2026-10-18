package comet

// "the feature i am imagining is called comet"

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACometIsNamedByItsRepositoryAndPlatform(t *testing.T) {
	n, err := Of("/comet/abcd-nl/grove/x86_64-linux")
	require.NoError(t, err)
	assert.Equal(t, Name{Owner: "abcd-nl", Repo: "grove", Platform: "x86_64-linux"}, n)
	assert.Equal(t, "abcd-nl/grove/x86_64-linux", n.Key())

	for _, path := range []string{"/comet/", "/comet/abcd-nl", "/comet/abcd-nl/grove", "/comet/abcd-nl/grove/", "/comet/abcd-nl/grove/x86_64-linux/more", "/comet//grove/x86_64-linux", "/comet/../grove/x86_64-linux", "/comet/abcd-nl/grove/windows"} {
		_, err := Of(path)
		assert.Error(t, err, path)
	}
}

func TestAmGroundNamesEachCometTheNodeKeeps(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "abcd-nl", "grove", "aarch64-darwin", "ground")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	require.NoError(t, KeepDigest(kept))
	// A build still landing is not a comet yet.
	require.NoError(t, os.WriteFile(kept+".new", []byte("half"), 0o755))

	comets, err := Held(dir)
	require.NoError(t, err)
	require.Len(t, comets, 1)
	assert.Equal(t, "abcd-nl/grove", comets[0]["repository"])
	assert.Equal(t, "aarch64-darwin", comets[0]["platform"])
	assert.Equal(t, int64(7), comets[0]["size"])
	sum := sha256.Sum256([]byte("a comet"))
	assert.Equal(t, hex.EncodeToString(sum[:]), comets[0]["digest"])

	// A node that has built none keeps none, and says so as an empty list.
	none, err := Held(filepath.Join(dir, "nothing"))
	require.NoError(t, err)
	assert.Empty(t, none)
}

// handler answers as a node does, in JSON, and builds nothing.
func handler() Handler {
	return Handler{
		Refuse: func(w http.ResponseWriter, status int, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		},
		Respond: func(w http.ResponseWriter, status int, data any) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(data)
		},
		Log: func(string, ...any) {},
	}
}

func TestACometIsServedWithItsDigestAndAskedForByIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kept := filepath.Join(home, ".qntx", "comets", "abcd-nl", "grove", "x86_64-linux", "ground")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	require.NoError(t, KeepDigest(kept))
	sum := sha256.Sum256([]byte("a comet"))
	h := handler()

	got := httptest.NewRecorder()
	h.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux", nil))
	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), got.Header().Get("X-Comet-Digest"))
	assert.Equal(t, "a comet", got.Body.String())

	// Asking whether it moved costs a header, not the binary.
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/comet/abcd-nl/grove/x86_64-linux", nil))
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), head.Header().Get("X-Comet-Digest"))
	assert.Empty(t, head.Body.String())

	none := httptest.NewRecorder()
	h.ServeHTTP(none, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/other/x86_64-linux", nil))
	assert.Equal(t, http.StatusNotFound, none.Code)

	// The digest alone: what ground's sky asks every five seconds.
	digest := httptest.NewRecorder()
	h.ServeHTTP(digest, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux?digest", nil))
	assert.Equal(t, http.StatusOK, digest.Code)
	assert.JSONEq(t, `{"digest":"`+hex.EncodeToString(sum[:])+`"}`, digest.Body.String())
}

func TestACometForAPlatformWithoutItsStubsIsRefusedWithWhy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	native, err := Native()
	require.NoError(t, err)
	other := "aarch64-darwin"
	if native == other {
		other = "x86_64-linux"
	}
	got := httptest.NewRecorder()
	handler().ServeHTTP(got, httptest.NewRequest(http.MethodPost, "/comet/abcd-nl/grove/"+other, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, got.Code)
	assert.Contains(t, got.Body.String(), "this node builds "+native+" natively")
}

func TestADarwinCometLinksTheStubsTheNodeHolds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := Stubs("aarch64-darwin")
	assert.ErrorContains(t, err, "this node does not hold")
	_, err = Stubs("x86_64-linux")
	assert.ErrorContains(t, err, "it links Darwin alone")

	stubs := filepath.Join(home, ".qntx", "darwin-stubs")
	require.NoError(t, os.MkdirAll(filepath.Join(stubs, "MacOSX.sdk"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(stubs, "aarch64-darwin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stubs, "MacOSX.sdk", "SDKSettings.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(stubs, "aarch64-darwin", "libgit2.dylib"), []byte("a dylib"), 0o644))
	held, err := Stubs("aarch64-darwin")
	require.NoError(t, err)
	assert.Equal(t, DarwinStubs{SDK: filepath.Join(stubs, "MacOSX.sdk"), Git2: filepath.Join(stubs, "aarch64-darwin", "libgit2.dylib")}, held)
	// Each Darwin platform carries the libgit2 of its own architecture.
	_, err = Stubs("x86_64-darwin")
	assert.ErrorContains(t, err, "x86_64-darwin")
}

func TestGroundsSourcesAreItsModulesAndNoneOfItsTests(t *testing.T) {
	src := t.TempDir()
	for _, f := range []string{"source/main.d", "source/ritual/run.d", "source/comet_test.d", "source/README.md", "tools/wind.d"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(src, f)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(src, f), []byte("module x;"), 0o644))
	}
	sources, err := Sources(src)
	require.NoError(t, err)
	assert.Equal(t, []string{"source/main.d", "source/ritual/run.d"}, sources)
}

func TestAPushRebuildsTheCometsItMoves(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"abcd-nl/grove/x86_64-linux", "abcd-nl/grove/aarch64-darwin", "abcd-nl/other/x86_64-linux"} {
		kept := filepath.Join(dir, filepath.FromSlash(key), "ground")
		require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
		require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	}
	kept, err := Kept(dir)
	require.NoError(t, err)
	require.Len(t, kept, 3)

	grove := MovedBy(kept, "abcd-nl/grove", "master", "master")
	assert.ElementsMatch(t, []Name{{"abcd-nl", "grove", "x86_64-linux"}, {"abcd-nl", "grove", "aarch64-darwin"}}, grove)
	// A branch that is not the project's default moves nothing.
	assert.Empty(t, MovedBy(kept, "abcd-nl/grove", "a-branch", "master"))
	// Ground's main moves every comet, since every comet is built from it.
	assert.Len(t, MovedBy(kept, "teranos/ground", "main", "main"), 3)
	assert.Empty(t, MovedBy(kept, "teranos/ground", "a-branch", "main"))
}

func TestACometsDigestIsWrittenOnceBesideIt(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "ground")
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	sum := sha256.Sum256([]byte("a comet"))

	require.NoError(t, KeepDigest(kept))
	written, err := os.ReadFile(kept + ".digest")
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), string(written))

	// Read from beside it, not hashed again.
	require.NoError(t, os.WriteFile(kept+".digest", []byte("said"), 0o644))
	said, err := Digest(kept)
	require.NoError(t, err)
	assert.Equal(t, "said", said)
}

func TestCometSandIsGroundsControlsAndTheProjectsAlone(t *testing.T) {
	// One project block naming the checkout: wind folds that project's controls and nothing of anyone's.
	// Its origin is the repository wherever it is checked out on the machine the comet lands on.
	assert.Equal(t, "project {\n  path: \"/b/grove\"\n  origin: \"abcd-nl/grove\"\n}\n", Declaration("/b/grove", "abcd-nl", "grove"))
}
