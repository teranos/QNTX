package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	s := &QNTXServer{logger: zap.NewNop().Sugar()}
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

func TestAPushMovesTheBuildsItsRepoAndBranchFeed(t *testing.T) {
	b := pluginBuild{
		name:   "datapunt",
		core:   buildSource{Owner: "teranos", Repo: "datapunt", Branch: "main"},
		inputs: []buildSource{{Owner: "abcd-nl", Repo: "clean", Branch: "main", Path: "competitor.cue"}},
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
		if got := push(c.raw).moves(b); got != c.moves {
			t.Errorf("%s: moves=%v", name, got)
		}
	}
}
