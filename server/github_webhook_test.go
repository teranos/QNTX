package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestSignedIsTheDeliverysHMAC(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !signed("s3cret", signature, body) {
		t.Fatal("the App's own signature was refused")
	}
	if signed("other", signature, body) {
		t.Fatal("a signature under another secret was taken")
	}
	if signed("s3cret", signature, []byte(`{"ref":"refs/heads/evil"}`)) {
		t.Fatal("a signature over another body was taken")
	}
	if signed("s3cret", "", body) {
		t.Fatal("an unsigned delivery was taken")
	}
}

func TestTheWebhookIsNoneUntilROOTGeneratesItsSecret(t *testing.T) {
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar()}
	w := httptest.NewRecorder()
	s.HandleGitHubWebhook(w, httptest.NewRequest(http.MethodPost, githubWebhookPath, strings.NewReader(`{}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("answered %d before a secret was generated", w.Code)
	}
}

func TestTheWebhooksPathIsUnderGitHub(t *testing.T) {
	for _, path := range []string{"/github/webhook", "/github/hooks/app"} {
		if err := webhookPath(path); err != nil {
			t.Errorf("%s refused: %v", path, err)
		}
	}
	for _, path := range []string{"", "/github/", "/api/github/webhook", "/github/web hook", "/github/x?y"} {
		if err := webhookPath(path); err == nil {
			t.Errorf("%q taken", path)
		}
	}
}

func TestAVTagLandingOnQNTXIsKnown(t *testing.T) {
	for raw, tag := range map[string]string{
		`{"ref":"refs/tags/v0.37.1","repository":{"full_name":"teranos/QNTX"}}`:                "v0.37.1",
		`{"ref":"refs/tags/v0.37.1","deleted":true,"repository":{"full_name":"teranos/QNTX"}}`: "",
		`{"ref":"refs/tags/branch-main-latest","repository":{"full_name":"teranos/QNTX"}}`:     "",
		`{"ref":"refs/heads/main","repository":{"full_name":"teranos/QNTX"}}`:                  "",
		`{"ref":"refs/tags/v1.0.0","repository":{"full_name":"teranos/datapunt"}}`:             "",
	} {
		var p gitHubPush
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		if got := tagLanded(p.push()); got != tag {
			t.Errorf("%s: landed %q", raw, got)
		}
	}
}

func TestAPushLandsOnTheFollowOfItsRepoAndBranch(t *testing.T) {
	f := GitHubFollow{Repo: "github.com/teranos/QNTX", Branch: "main", Dispatches: "github.com/sbvh-nl/q.sbvh.nl", Workflow: "deploy.yml", Ref: "master"}
	for name, c := range map[string]struct {
		raw     string
		enabled bool
		follows bool
	}{
		"main of the repo":         {`{"ref":"refs/heads/main","repository":{"full_name":"teranos/QNTX"}}`, true, true},
		"main, turned off":         {`{"ref":"refs/heads/main","repository":{"full_name":"teranos/QNTX"}}`, false, false},
		"another branch":           {`{"ref":"refs/heads/wip","repository":{"full_name":"teranos/QNTX"}}`, true, false},
		"a tag named main":         {`{"ref":"refs/tags/main","repository":{"full_name":"teranos/QNTX"}}`, true, false},
		"main deleted":             {`{"ref":"refs/heads/main","deleted":true,"repository":{"full_name":"teranos/QNTX"}}`, true, false},
		"main of another repo":     {`{"ref":"refs/heads/main","repository":{"full_name":"teranos/datapunt"}}`, true, false},
		"the repo in another case": {`{"ref":"refs/heads/main","repository":{"full_name":"Teranos/qntx"}}`, true, true},
	} {
		var p gitHubPush
		if err := json.Unmarshal([]byte(c.raw), &p); err != nil {
			t.Fatal(err)
		}
		f.Enabled = c.enabled
		got, err := followed(p.push(), f)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.follows {
			t.Errorf("%s: follows=%v", name, got)
		}
	}
}

func TestAPushMovesTheBuildsItsRepoAndBranchFeed(t *testing.T) {
	b := pluginBuild{
		name:   "datapunt",
		core:   buildSource{Repo: forge.Repo{Host: forge.GitHub, Path: "teranos/datapunt"}, Branch: "main"},
		inputs: []buildSource{{Repo: forge.Repo{Host: forge.GitHub, Path: "abcd-nl/clean"}, Branch: "main", Path: "competitor.cue", PathNamed: true}},
	}
	push := func(raw string) gitHubPush {
		var p gitHubPush
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, c := range map[string]struct {
		raw   string
		moves bool
	}{
		"the core's branch":          {`{"ref":"refs/heads/main","repository":{"full_name":"teranos/datapunt"}}`, true},
		"another branch of the core": {`{"ref":"refs/heads/wip","repository":{"full_name":"teranos/datapunt"}}`, false},
		"a tag":                      {`{"ref":"refs/tags/main","repository":{"full_name":"teranos/datapunt"}}`, false},
		"the input's file": {`{"ref":"refs/heads/main","repository":{"full_name":"abcd-nl/clean"},
			"commits":[{"modified":["competitor.cue"]}]}`, true},
		"another file of the input": {`{"ref":"refs/heads/main","repository":{"full_name":"abcd-nl/clean"},
			"commits":[{"modified":["README.md"]}]}`, false},
		"an unrelated repo": {`{"ref":"refs/heads/main","repository":{"full_name":"teranos/QNTX"}}`, false},
	} {
		if got := pushMoves(push(c.raw).push(), b); got != c.moves {
			t.Errorf("%s: moves=%v", name, got)
		}
	}
}

// The same owner and name on another forge is another repository.
func TestAPushToTheSameNameOnAnotherForgeMovesNothing(t *testing.T) {
	b := pluginBuild{
		name: "datapunt",
		core: buildSource{Repo: forge.Repo{Host: forge.GitHub, Path: "teranos/datapunt"}, Branch: "main"},
	}
	elsewhere := forge.Push{Repo: forge.Repo{Host: "gitlab.com", Path: "teranos/datapunt"}, Ref: "refs/heads/main"}
	if pushMoves(elsewhere, b) {
		t.Errorf("a push to %s moved a build of %s", elsewhere.Repo, b.core.Repo)
	}
	f := GitHubFollow{Repo: "github.com/teranos/datapunt", Branch: "main", Enabled: true}
	got, err := followed(elsewhere, f)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Errorf("a push to %s landed on a follow of %s", elsewhere.Repo, f.Repo)
	}
}
