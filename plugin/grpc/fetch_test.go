package grpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func testLogger(t *testing.T) *zap.SugaredLogger {
	t.Helper()
	return zap.NewNop().Sugar()
}

func TestGithubRepoPath(t *testing.T) {
	tests := []struct {
		repo      string
		wantOwner string
		wantRepo  string
		wantErr   string
	}{
		{repo: "https://github.com/sbvh-nl/duif", wantOwner: "sbvh-nl", wantRepo: "duif"},
		{repo: "https://github.com/sbvh-nl/duif.git", wantOwner: "sbvh-nl", wantRepo: "duif"},
		{repo: "https://github.com/teranos/pty-glyph", wantOwner: "teranos", wantRepo: "pty-glyph"},
		{repo: "https://codeberg.org/sbvh-nl/duif", wantErr: "unsupported plugin source host"},
		{repo: "https://gitlab.com/sbvh-nl/duif", wantErr: "unsupported plugin source host"},
		{repo: "https://github.com/sbvh-nl", wantErr: "not an owner/repo path"},
		{repo: "https://github.com/sbvh-nl/duif/releases", wantErr: "not an owner/repo path"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			owner, repo, err := githubRepoPath(tt.repo)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("githubRepoPath(%q) = %q/%q, want error %q", tt.repo, owner, repo, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("githubRepoPath(%q) error = %v, want it to mention %q", tt.repo, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("githubRepoPath(%q) = %v", tt.repo, err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("githubRepoPath(%q) = %q/%q, want %q/%q", tt.repo, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// The rejection message must name the host the operator actually wrote, so the
// fix is obvious from the log line alone.
func TestUnsupportedHostErrorNamesTheHost(t *testing.T) {
	_, _, err := githubRepoPath("https://codeberg.org/sbvh-nl/duif")
	if err == nil {
		t.Fatal("githubRepoPath accepted a non-github host")
	}
	if !strings.Contains(err.Error(), "codeberg.org") {
		t.Errorf("error does not name the host: %v", err)
	}
}

func TestPluginBinaryName(t *testing.T) {
	if got := PluginBinaryName("duif"); got != "qntx-duif-plugin" {
		t.Errorf("PluginBinaryName(duif) = %q, want qntx-duif-plugin", got)
	}
}

func TestFindAsset(t *testing.T) {
	rel := release{
		TagName: "v1.2.0",
		Assets: []releaseAsset{
			{Name: "qntx-duif-plugin-linux-amd64.tar.gz"},
			{Name: "qntx-duif-plugin-linux-amd64.tar.gz.sha256"},
			{Name: "qntx-duif-plugin-darwin-arm64.tar.gz"},
		},
	}

	got, ok := findAsset(rel, "-darwin-arm64.tar.gz")
	if !ok {
		t.Fatal("findAsset found no darwin-arm64 asset")
	}
	if got.Name != "qntx-duif-plugin-darwin-arm64.tar.gz" {
		t.Errorf("findAsset = %q, want the darwin-arm64 asset", got.Name)
	}

	if _, ok := findAsset(rel, "-windows-amd64.tar.gz"); ok {
		t.Error("findAsset matched a platform the release does not publish")
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("\x7fELF pretend this is a plugin")
	archive := tarGz(t, map[string][]byte{
		"README.md":                  []byte("docs"),
		"dist/qntx-duif-plugin":      want,
		"dist/qntx-duif-plugin.dSYM": []byte("debug symbols"),
	})

	got, err := extractBinary(archive, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("extractBinary = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extractBinary returned %q, want %q", got, want)
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": []byte("docs")})

	_, err := extractBinary(archive, "qntx-duif-plugin")
	if err == nil {
		t.Fatal("extractBinary accepted an archive with no plugin binary")
	}
	// The error lists what was there, so the operator can see what shipped.
	if !strings.Contains(err.Error(), "README.md") {
		t.Errorf("error does not list the archive contents: %v", err)
	}
}

func TestInstallIsExecutableAndReplaces(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "plugins", "qntx-duif-plugin")

	if err := install([]byte("first"), dest); err != nil {
		t.Fatalf("install = %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat %s = %v", dest, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed binary %s is not executable (mode %v)", dest, info.Mode())
	}

	// A read-only previous install must not block a replacement.
	if err := os.Chmod(dest, 0o555); err != nil {
		t.Fatalf("chmod = %v", err)
	}
	if err := install([]byte("second"), dest); err != nil {
		t.Fatalf("install over an existing read-only binary = %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read %s = %v", dest, err)
	}
	if string(data) != "second" {
		t.Errorf("installed content = %q, want %q", data, "second")
	}

	// Nothing partial may be left behind.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("readdir = %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial-") {
			t.Errorf("install left a partial file behind: %s", entry.Name())
		}
	}
}

// A binary whose bytes disagree with the published digest is never installed.
func TestFetchPluginRejectsChecksumMismatch(t *testing.T) {
	binary := []byte("\x7fELF the real plugin")
	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": binary})

	srv := releaseServer(t, archive, strings.Repeat("0", sha256.Size*2), "")
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := fetchPlugin(context.Background(), "duif", "https://github.com/sbvh-nl/duif", testLogger(t))
	if err == nil {
		t.Fatal("fetchPlugin installed a binary that failed its checksum")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}

	dest := filepath.Join(home, ".qntx", "plugins", "qntx-duif-plugin")
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Errorf("a binary that failed verification was installed at %s", dest)
	}
}

func TestFetchPluginInstallsVerifiedBinary(t *testing.T) {
	binary := []byte("\x7fELF the real plugin")
	archive := tarGz(t, map[string][]byte{"dist/qntx-duif-plugin": binary})

	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])

	srv := releaseServer(t, archive, digest, "")
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := fetchPlugin(context.Background(), "duif", "https://github.com/sbvh-nl/duif", testLogger(t))
	if err != nil {
		t.Fatalf("fetchPlugin = %v", err)
	}

	want := filepath.Join(home, ".qntx", "plugins", "qntx-duif-plugin")
	if got != want {
		t.Errorf("fetchPlugin installed at %q, want %q", got, want)
	}

	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read %s = %v", got, err)
	}
	if !bytes.Equal(data, binary) {
		t.Errorf("installed bytes do not match the archived binary")
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat = %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed binary is not executable (mode %v)", info.Mode())
	}
}

// A release that ships an asset without its .sha256 is not installable —
// there is nothing to verify against.
func TestFetchPluginRequiresPublishedChecksum(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("bytes")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := release{
			TagName: "v1.0.0",
			Assets: []releaseAsset{{
				Name: "qntx-duif-plugin" + pluginAssetSuffix(),
				URL:  "http://" + r.Host + "/asset",
				Size: int64(len(archive)),
			}},
		}
		json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = oldBase }()

	t.Setenv("HOME", t.TempDir())

	_, err := fetchPlugin(context.Background(), "duif", "https://github.com/sbvh-nl/duif", testLogger(t))
	if err == nil {
		t.Fatal("fetchPlugin installed an asset with no published checksum")
	}
	if !strings.Contains(err.Error(), ".sha256") {
		t.Errorf("error = %v, want it to name the missing .sha256", err)
	}
}

// A private repo answers 404 without credentials; the error must say so rather
// than read as "this plugin does not exist".
func TestFetchPluginPrivateRepoHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = oldBase }()

	t.Setenv("HOME", t.TempDir())

	_, err := fetchPlugin(context.Background(), "duif", "https://github.com/sbvh-nl/duif", testLogger(t))
	if err == nil {
		t.Fatal("fetchPlugin succeeded against a 404")
	}

	// The fix lives in the hint, which is what formatHints puts in the log line.
	if !strings.Contains(formatHints(err), "access_token") {
		t.Errorf("hints = %q, want them to point at [plugin.access_token] (error: %v)", formatHints(err), err)
	}
}

// releaseServer serves one release whose single platform asset is archive,
// with digest published alongside it. When token is non-empty, every request
// must carry it.
func releaseServer(t *testing.T, archive []byte, digest, token string) *httptest.Server {
	t.Helper()

	assetName := "qntx-duif-plugin" + pluginAssetSuffix()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}

		switch r.URL.Path {
		case "/asset":
			w.Write(archive)
		case "/asset.sha256":
			fmt.Fprintf(w, "%s  %s\n", digest, assetName)
		default:
			rel := release{
				TagName: "v1.0.0",
				Assets: []releaseAsset{
					{Name: assetName, URL: "http://" + r.Host + "/asset", Size: int64(len(archive))},
					{Name: assetName + ".sha256", URL: "http://" + r.Host + "/asset.sha256"},
				},
			}
			json.NewEncoder(w).Encode(rel)
		}
	}))

	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	return srv
}

// tarGz builds a .tar.gz holding the given paths.
func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		header := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar body for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.Bytes()
}
