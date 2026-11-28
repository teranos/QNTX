package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// "and what get's sent to sentry or anything else , is the user experience of an error,"
// "something that makes it easy for us to see and fix, because the experience being bad is immediatly clear and why"
// 「[Opus 5.5] A request that failed on the node's side is reported as the Error the person」
// 「[Opus 5.5] was answered with; a refusal is an answer, and a 503 a state.」
func TestWhatAPersonWasAnsweredWithIsReportedAsTheySawIt(t *testing.T) {
	transport := &sentry.MockTransport{}
	if err := errors.Report(errors.Reporting{Client: sentry.ClientOptions{Dsn: "https://key@example.com/1", Transport: transport}, PerDay: 100}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	for _, status := range []int{http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable} {
		writeErrorEnvelope(httptest.NewRecorder(), zap.NewNop().Sugar(), "pulse", errors.New("the store is gone"), status)
	}
	if got := len(transport.Events()); got != 0 {
		t.Fatalf("answers and states raised %d reports, want 0", got)
	}

	w := httptest.NewRecorder()
	writeErrorEnvelope(w, zap.NewNop().Sugar(), "pulse", errors.New("the store is gone"), http.StatusInternalServerError)
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("a failure a person was answered with raised %d reports, want 1", len(events))
	}
	if events[0].Message != "the store is gone" || events[0].Tags["surface"] != "pulse" {
		t.Errorf("the report reads %q at %q, not as the person saw it", events[0].Message, events[0].Tags["surface"])
	}
	if id := w.Header().Get("X-QNTX-Error-Id"); id == "" || events[0].Tags["error_id"] != id {
		t.Errorf("the report is %q, the person was shown %q", events[0].Tags["error_id"], id)
	}
}

// 「[Opus 5.5] Nothing in QNTX raises an issue by Sentry's own hand: sacred-error does.」
func TestNothingInQNTXCallsSentrysCapture(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "target", "vendor", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, capture := range []string{"CaptureException(", "CaptureMessage(", "CaptureEvent("} {
			if strings.Contains(string(body), capture) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, rel+": "+capture)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s: an issue is sacred-error's to raise (errors.Shown)", f)
	}
}
