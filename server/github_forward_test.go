package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// signedDelivery is a delivery as GitHub would hand it to the node.
func signedDelivery(t *testing.T, secret, event, guid string, body []byte) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	r := httptest.NewRequest(http.MethodPost, githubWebhookPath, bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", event)
	r.Header.Set("X-GitHub-Delivery", guid)
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func TestAForwardIsTheNewestLineOfItsURLAndOffIsNotAForward(t *testing.T) {
	s := githubKnowingServer(t)
	records := s.nodeRecords()
	require.NoError(t, records.SetForward(rootAccount, GitHubForward{URL: "http://10.88.0.2:8770/github/webhook", Enabled: true}))

	forwards, err := records.Forwards()
	require.NoError(t, err)
	require.Len(t, forwards, 1)
	assert.Equal(t, "http://10.88.0.2:8770/github/webhook", forwards[0].URL)

	require.NoError(t, records.SetForward(rootAccount, GitHubForward{URL: "http://10.88.0.2:8770/github/webhook", Enabled: false}))
	forwards, err = records.Forwards()
	require.NoError(t, err)
	assert.Empty(t, forwards)
}

func TestAForwardNamesAnHTTPURLWithAHost(t *testing.T) {
	for _, url := range []string{"http://10.88.0.2:8770/github/webhook", "https://k.abcd.nl/github/webhook"} {
		assert.NoError(t, forwardURL(url), url)
	}
	for _, url := range []string{"", "10.88.0.2:8770/github/webhook", "ftp://k/github/webhook", "http:///github/webhook"} {
		assert.Error(t, forwardURL(url), url)
	}
}

// What the far node verifies is what GitHub signed: the body and the signature
// arrive untouched, so a node holding the App's secret takes the delivery.
func TestAForwardCarriesTheDeliveryUntouched(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	var got *http.Request
	var gotBody []byte
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer far.Close()

	job, err := forwardJob(far.URL, "push", "guid-1", "sha256=abc", body)
	require.NoError(t, err)
	require.NoError(t, (&forwardHandler{client: far.Client(), logger: zap.NewNop().Sugar()}).Execute(context.Background(), job))

	assert.Equal(t, body, gotBody)
	assert.Equal(t, "push", got.Header.Get("X-GitHub-Event"))
	assert.Equal(t, "guid-1", got.Header.Get("X-GitHub-Delivery"))
	assert.Equal(t, "sha256=abc", got.Header.Get("X-Hub-Signature-256"))
}

// A status the far node gave itself is an answer, and sending it again changes
// nothing. Only silence is swept.
func TestOnlyAForwardNobodyAnsweredIsSentAgain(t *testing.T) {
	status := http.StatusServiceUnavailable
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer far.Close()
	queue := async.NewQueue(qntxtest.CreateTestDB(t))
	handler := &forwardHandler{client: far.Client(), logger: zap.NewNop().Sugar()}

	fail := func(guid string) string {
		job, err := forwardJob(far.URL, "push", guid, "sha256=abc", []byte(`{}`))
		require.NoError(t, err)
		require.NoError(t, queue.Enqueue(job))
		failure := handler.Execute(context.Background(), job)
		require.Error(t, failure)
		require.NoError(t, queue.FailJob(job.ID, failure))
		return job.ID
	}
	silent := fail("silent")
	status = http.StatusNotFound
	answered := fail("answered")

	swept, err := sweepForwards(queue, zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.Equal(t, 1, swept)

	job, err := queue.GetJob(silent)
	require.NoError(t, err)
	assert.Equal(t, async.JobStatusQueued, job.Status)
	job, err = queue.GetJob(answered)
	require.NoError(t, err)
	assert.Equal(t, async.JobStatusFailed, job.Status)
}

// The forward is written before the delivery is handled, so the far node gets
// what this node goes on to be slow over.
func TestTheWebhookForwardsWhatItVerifiedAndNothingElse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := artifactServingServer(t, nil, nil)
	secret, err := s.authHandler.NewGitHubWebhook(rootAccount)
	require.NoError(t, err)
	require.NoError(t, s.nodeRecords().SetForward(rootAccount, GitHubForward{URL: "http://10.88.0.2:8770/github/webhook", Enabled: true}))
	body := []byte(`{"action":"completed","workflow_run":{"id":9,"conclusion":"failure","created_at":"2026-10-12T10:00:00Z"},"repository":{"full_name":"teranos/pyre"}}`)

	w := httptest.NewRecorder()
	s.HandleGitHubWebhook(w, signedDelivery(t, "not the secret", "workflow_run", "forged", body))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	jobs, err := s.daemon.GetQueue().ListEveryJob(10)
	require.NoError(t, err)
	assert.Empty(t, jobs, "a delivery the node refused went on to another node")

	w = httptest.NewRecorder()
	s.HandleGitHubWebhook(w, signedDelivery(t, secret, "workflow_run", "guid-2", body))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	jobs, err = s.daemon.GetQueue().ListEveryJob(10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, forwardHandlerName, jobs[0].HandlerName)
	assert.Equal(t, async.JobStatusQueued, jobs[0].Status)
}

// A node the App's webhook does not point at holds the secret of the node it does.
func TestANodeTakesTheSecretOfTheNodeThatForwardsToIt(t *testing.T) {
	s := githubKnowingServer(t)
	require.NoError(t, s.authHandler.SetGitHubWebhook(rootAccount, "the secret q holds"))
	secret, active, err := s.authHandler.GitHubWebhook()
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "the secret q holds", secret)
}
