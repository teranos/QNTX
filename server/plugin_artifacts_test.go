package server

// A plugin built on another machine reaches the node as an artifact of a
// successful workflow run in its record's repository (ADR-043).

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// pyreBuild is the archive name pyre's package step writes for this platform.
func pyreBuild(version string) string {
	return "qntx-pyre-plugin-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
}

// pyreArtifact is the artifact pyre's workflow uploads: its build of binary
// and the .sha256 beside it.
func pyreArtifact(t *testing.T, version string, binary []byte) []byte {
	t.Helper()
	built := filepath.Join(t.TempDir(), "qntx-pyre-plugin")
	require.NoError(t, os.WriteFile(built, binary, 0o755))
	archive, err := grpcplugin.PackageBuild("pyre", built)
	require.NoError(t, err)
	sum := sha256.Sum256(archive)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{
		pyreBuild(version):             archive,
		pyreBuild(version) + ".sha256": []byte(hex.EncodeToString(sum[:]) + "  " + pyreBuild(version) + "\n"),
	} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// artifactServingServer is a node holding the App with pyre enabled, whose
// GitHub has the App installed on teranos/pyre, lists each run's artifacts
// from runs and serves each artifact from zips at a signed URL.
func artifactServingServer(t *testing.T, runs map[int64][]map[string]any, zips map[int64][]byte) (*QNTXServer, func() []string) {
	t.Helper()
	s := githubKnowingServer(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s.authHandler.SetGitHubApp("Iv23li-the-app", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	_, err = s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.NoError(t, err)
	require.NoError(t, s.pluginRecords().EnablePlugin(rootAccount, "pyre", true))

	var mu sync.Mutex
	var asked []string
	idIn := func(path, prefix, suffix string) int64 {
		id, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix), 10, 64)
		require.NoError(t, err)
		return id
	}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.Method + " " + r.URL.Path
		mu.Lock()
		asked = append(asked, route)
		mu.Unlock()
		switch {
		case route == "GET /repos/teranos/pyre/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": "the-app"})
		case route == "POST /app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_minted", "expires_at": "2099-01-01T00:00:00Z",
				"permissions": map[string]string{"actions": "read"}})
		case strings.HasPrefix(route, "GET /repos/teranos/pyre/actions/runs/") && strings.HasSuffix(route, "/artifacts"):
			listed := runs[idIn(r.URL.Path, "/repos/teranos/pyre/actions/runs/", "/artifacts")]
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(listed), "artifacts": listed})
		case strings.HasPrefix(route, "GET /repos/teranos/pyre/actions/artifacts/") && strings.HasSuffix(route, "/zip"):
			id := idIn(r.URL.Path, "/repos/teranos/pyre/actions/artifacts/", "/zip")
			http.Redirect(w, r, "/signed/"+strconv.FormatInt(id, 10), http.StatusFound)
		case strings.HasPrefix(route, "GET /signed/"):
			_, _ = w.Write(zips[idIn(r.URL.Path, "/signed/", "")])
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found: " + route})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, asked...)
	}
}

// workflowRun is a workflow_run delivery, as GitHub sends it.
func workflowRun(t *testing.T, repo string, id int64, action, conclusion, createdAt string) gitHubWorkflowRun {
	t.Helper()
	var run gitHubWorkflowRun
	require.NoError(t, json.Unmarshal([]byte(`{"action":"`+action+`","workflow_run":{"id":`+strconv.FormatInt(id, 10)+
		`,"head_sha":"c0ffee","status":"completed","conclusion":"`+conclusion+`","created_at":"`+createdAt+
		`","html_url":"https://github.com/`+repo+`/actions/runs/`+strconv.FormatInt(id, 10)+`"},"repository":{"full_name":"`+repo+`"}}`), &run))
	return run
}

// installedPyre is the binary installed as pyre.
func installedPyre(t *testing.T) string {
	t.Helper()
	dir, err := grpcplugin.PluginInstallPath("pyre")
	require.NoError(t, err)
	held, err := os.ReadFile(filepath.Join(dir, grpcplugin.PluginBinaryName("pyre")))
	require.NoError(t, err)
	return string(held)
}

// artifactsShown is what the GitHub element is given of the builds taken from
// workflow runs.
func artifactsShown(t *testing.T, s *QNTXServer) []*protocol.TakenBuild {
	t.Helper()
	said, refused := answer(t, s, "status", sigil.Sent{})
	require.Nil(t, refused)
	return said.(*protocol.GitHubStatus).GetArtifacts()
}

// Of a run's artifacts, the one that is pyre's build for this platform and
// has not expired is downloaded, checked and installed.
func TestARunsBuildIsInstalledFromItsArtifact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, asked := artifactServingServer(t, map[int64][]map[string]any{9: {
		{"id": 1, "name": pyreBuild("0.9.0"), "expired": true},
		{"id": 2, "name": "qntx-pyre-plugin-1.0.0-plan9-mips.tar.gz", "expired": false},
		{"id": 3, "name": pyreBuild("1.0.0"), "expired": false},
		{"id": 4, "name": "coverage", "expired": false},
	}}, map[int64][]byte{3: pyreArtifact(t, "1.0.0", []byte("\x7fELF pyre 1.0.0"))})

	run := workflowRun(t, "teranos/pyre", 9, "completed", "success", "2026-10-12T10:00:00Z")
	require.NoError(t, s.takeRun(context.Background(), "pyre", gitHubSource{Owner: "teranos", Repo: "pyre"}, run))

	assert.Equal(t, "\x7fELF pyre 1.0.0", installedPyre(t))
	assert.Contains(t, asked(), "GET /repos/teranos/pyre/actions/artifacts/3/zip")
	for _, id := range []string{"1", "2", "4"} {
		assert.NotContains(t, asked(), "GET /repos/teranos/pyre/actions/artifacts/"+id+"/zip")
	}

	shown := artifactsShown(t, s)
	require.Len(t, shown, 1)
	assert.Equal(t, "pyre", shown[0].Plugin)
	assert.Equal(t, pyreBuild("1.0.0"), shown[0].Archive)
	assert.Equal(t, "https://github.com/teranos/pyre/actions/runs/9", shown[0].Run)
	assert.True(t, shown[0].Changed)
	assert.False(t, shown[0].Older)
}

// The newest run's build is the one that runs: a delivery of an older run,
// arriving late or redelivered, is not installed over it.
func TestARunOlderThanTheInstalledOneIsNotInstalledOverIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := artifactServingServer(t, map[int64][]map[string]any{
		9:  {{"id": 3, "name": pyreBuild("1.0.0"), "expired": false}},
		10: {{"id": 5, "name": pyreBuild("1.0.1"), "expired": false}},
	}, map[int64][]byte{
		3: pyreArtifact(t, "1.0.0", []byte("\x7fELF pyre 1.0.0")),
		5: pyreArtifact(t, "1.0.1", []byte("\x7fELF pyre 1.0.1")),
	})
	pyre := gitHubSource{Owner: "teranos", Repo: "pyre"}

	require.NoError(t, s.takeRun(context.Background(), "pyre", pyre, workflowRun(t, "teranos/pyre", 10, "completed", "success", "2026-10-12T11:00:00Z")))
	require.NoError(t, s.takeRun(context.Background(), "pyre", pyre, workflowRun(t, "teranos/pyre", 9, "completed", "success", "2026-10-12T10:00:00Z")))

	assert.Equal(t, "\x7fELF pyre 1.0.1", installedPyre(t))
	shown := artifactsShown(t, s)
	require.Len(t, shown, 2)
	assert.Equal(t, "https://github.com/teranos/pyre/actions/runs/9", shown[1].Run)
	assert.True(t, shown[1].Older)
	assert.False(t, shown[1].Changed)
}

// Nothing is downloaded from a repository no enabled plugin's record names,
// nor from a run that did not succeed.
func TestOnlyASuccessfulRunOfAnEnabledPluginsRepositoryIsTaken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, asked := artifactServingServer(t, nil, nil)
	_, err := s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/duif")
	require.NoError(t, err)

	for name, run := range map[string]gitHubWorkflowRun{
		"a run that failed":               workflowRun(t, "teranos/pyre", 9, "completed", "failure", "2026-10-12T10:00:00Z"),
		"a run that only started":         workflowRun(t, "teranos/pyre", 9, "requested", "", "2026-10-12T10:00:00Z"),
		"a disabled plugin's repository":  workflowRun(t, "teranos/duif", 9, "completed", "success", "2026-10-12T10:00:00Z"),
		"a repository no record names":    workflowRun(t, "teranos/elsewhere", 9, "completed", "success", "2026-10-12T10:00:00Z"),
		"a repository named like another": workflowRun(t, "teranos/pyre-tools", 9, "completed", "success", "2026-10-12T10:00:00Z"),
	} {
		taking, err := s.buildsOfRun(run)
		require.NoError(t, err, name)
		assert.Empty(t, taking, name)
	}
	assert.Empty(t, asked())
}

// A run's build that was not installed is said where the builds are shown:
// the take runs on its own, with no caller to hand the error to.
func TestARunsBuildNotInstalledIsSaidWhereTheBuildsAreShown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := artifactServingServer(t, map[int64][]map[string]any{
		9: {{"id": 3, "name": pyreBuild("1.0.0"), "expired": false}},
	}, map[int64][]byte{3: []byte("not a zip")})

	run := workflowRun(t, "teranos/pyre", 9, "completed", "success", "2026-10-12T10:00:00Z")
	taking, err := s.buildsOfRun(run)
	require.NoError(t, err)
	assert.Equal(t, []string{"pyre"}, taking)

	require.Eventually(t, func() bool {
		said, refused := answer(t, s, "status", sigil.Sent{})
		return refused == nil && len(said.(*protocol.GitHubStatus).GetArtifactFaults()) == 1
	}, 5*time.Second, 10*time.Millisecond)
	said, _ := answer(t, s, "status", sigil.Sent{})
	fault := said.(*protocol.GitHubStatus).GetArtifactFaults()[0]
	assert.Contains(t, fault, "pyre")
	assert.Contains(t, fault, "https://github.com/teranos/pyre/actions/runs/9")
	assert.Contains(t, fault, "not a zip")
}

// A plugin's repository is matched as GitHub names it, whatever its case.
func TestARunIsOfThePluginsWhoseRepositoryItIsIn(t *testing.T) {
	records := []grpcplugin.PluginRecord{
		{Name: "pyre", Repo: "https://github.com/teranos/pyre", Enabled: true},
		{Name: "kern", Repo: "https://github.com/teranos/QNTX/tree/main/qntx-plugins/kern", Enabled: true},
		{Name: "duif", Repo: "https://github.com/teranos/duif", Enabled: false},
		{Name: "elsewhere", Repo: "https://gitlab.com/teranos/pyre", Enabled: true},
	}
	ran := func(repo string) []string {
		names := []string{}
		for _, of := range pluginsOfRun(records, repo) {
			names = append(names, of.name)
		}
		return names
	}
	assert.Equal(t, []string{"pyre"}, ran("Teranos/Pyre"))
	assert.Equal(t, []string{"kern"}, ran("teranos/QNTX"))
	assert.Empty(t, ran("teranos/duif"))
}

// The App's workflow_run delivery is read for the builds it carries.
func TestTheWebhookReadsAWorkflowRunForBuilds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := artifactServingServer(t, nil, nil)
	secret, err := s.authHandler.NewGitHubWebhook(rootAccount)
	require.NoError(t, err)

	body := []byte(`{"action":"completed","workflow_run":{"id":9,"conclusion":"failure","created_at":"2026-10-12T10:00:00Z"},"repository":{"full_name":"teranos/pyre"}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	r := httptest.NewRequest(http.MethodPost, githubWebhookPath, bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", "workflow_run")
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	s.HandleGitHubWebhook(w, r)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"taking": []}`, w.Body.String())
}
