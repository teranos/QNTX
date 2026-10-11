package server

// Approvals (ADR-052). "approvals are attestations obviously" "Approvals is
// human only" "And only through it's element" "approvals is ROOT only for
// now" "APPROVALS INTEGRATE DIRECTLY WITH THE GITHUB APP"

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// fakeGitHub is what the stand-in GitHub holds: whether main is green, and
// every merge it was asked for.
type fakeGitHub struct {
	mainGreen bool
	merges    []string
}

// approvingServer is a node holding the App, whose GitHub is a stand-in with
// the App installed on teranos/elements: main's check suites answer from
// fake.mainGreen, and a merge is taken and remembered.
func approvingServer(t *testing.T) (*QNTXServer, *fakeGitHub) {
	t.Helper()
	s := githubKnowingServer(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s.authHandler.SetGitHubApp("Iv23li-the-app", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))

	fake := &fakeGitHub{}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := r.Method + " " + r.URL.Path
		switch {
		case route == "GET /repos/teranos/elements/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": "the-app"})
		case route == "POST /app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_minted", "expires_at": "2099-01-01T00:00:00Z",
				"permissions": map[string]string{"contents": "write", "pull_requests": "write"}})
		case route == "GET /repos/teranos/elements/commits/main/check-runs":
			status, conclusion := "in_progress", ""
			if fake.mainGreen {
				status, conclusion = "completed", "success"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 2, "check_runs": []map[string]any{
				{"id": 1, "name": "TypeScript", "head_sha": "35174ca", "status": "completed", "conclusion": "success"},
				{"id": 2, "name": "Browser", "head_sha": "35174ca", "status": status, "conclusion": conclusion},
			}})
		case strings.HasPrefix(route, "PUT /repos/teranos/elements/pulls/") && strings.HasSuffix(route, "/merge"):
			fake.merges = append(fake.merges, string(body))
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "m3rg3d", "merged": true, "message": "Pull Request successfully merged"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found: " + route})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s, fake
}

// delivered hands the node one delivery of the App's webhook, as the handler
// would after checking its signature.
func delivered(t *testing.T, s *QNTXServer, event, body string) []string {
	t.Helper()
	touched, err := s.approvalsFromGitHub(context.Background(), event, []byte(body))
	require.NoError(t, err)
	return touched
}

func pullRequest(action string, number int, sha string, more string) string {
	return `{"action":"` + action + `","number":` + itoa(number) + `,"pull_request":{"title":"A panel on a phone reads at a phone's size",` +
		`"html_url":"https://github.com/teranos/elements/pull/` + itoa(number) + `","head":{"sha":"` + sha + `","ref":"claude/panel"},"base":{"ref":"main"},` +
		`"user":{"login":"teranos"}` + more + `},"repository":{"full_name":"teranos/elements","default_branch":"main"}}`
}

func checkRun(name, sha, status, conclusion string) string {
	return `{"action":"completed","check_run":{"name":"` + name + `","head_sha":"` + sha + `","status":"` + status + `","conclusion":"` + conclusion +
		`","html_url":"https://github.com/teranos/elements/runs/1"},"repository":{"full_name":"teranos/elements"}}`
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + json.Number(itoa64(n)).String())
}

func itoa64(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func listed(t *testing.T, s *QNTXServer) []*protocol.Approval {
	t.Helper()
	answered, refused := s.approvalsSignum().Answers["list"](asRoot(), sigil.Sent{})
	require.Nil(t, refused)
	return answered.(*protocol.Approvals).GetApprovals()
}

// press is one press of the element, by whoever ctx admits.
func press(s *QNTXServer, ctx context.Context, subject, sha, option, said string) (int, map[string]any) {
	body := `{"subject":"` + subject + `","sha":"` + sha + `","option":"` + option + `","said":"` + said + `"}`
	r := httptest.NewRequest(http.MethodPost, approvalsAnswerPath, strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	s.HandleApprovalAnswer(w, r)
	var answered map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &answered)
	return w.Code, answered
}

// checksPass tells the node every check on the head passed.
func checksPass(t *testing.T, s *QNTXServer, sha string) {
	t.Helper()
	for _, name := range []string{"TypeScript", "Browser"} {
		delivered(t, s, "check_run", checkRun(name, sha, "completed", "success"))
	}
}

// "The context in which you need to think of PR approvals is that these are
// always targeting main."
func TestAPullRequestAgainstMainWaitsOnROOT(t *testing.T) {
	s, _ := approvingServer(t)

	assert.Equal(t, []string{"teranos/elements#29"}, delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", "")))
	open := listed(t, s)
	require.Len(t, open, 1)
	assert.Equal(t, "teranos/elements#29", open[0].GetSubject())
	assert.Equal(t, "abe92a9", open[0].GetSha())
	assert.Equal(t, "main", open[0].GetBase())
	assert.Equal(t, int64(29), open[0].GetPull())
	assert.Equal(t, "https://github.com/teranos/elements/pull/29", open[0].GetLink())
	assert.Empty(t, open[0].GetSaid())

	// A draft, and a pull request against another branch, wait on nobody.
	assert.Empty(t, delivered(t, s, "pull_request", pullRequest("opened", 30, "d4af7", `,"draft":true`)))
	assert.Empty(t, delivered(t, s, "pull_request", strings.Replace(pullRequest("opened", 31, "0ff", ""), `"base":{"ref":"main"}`, `"base":{"ref":"release"}`, 1)))
	assert.Len(t, listed(t, s), 1)
}

// "The checks are what occur on the PR itself, before we even want to present
// it as something to approve or not." "Failed is RED. And FAILED isn't ready
// for approvals."
func TestTheChecksOnTheHeadGateTheDecision(t *testing.T) {
	s, _ := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	delivered(t, s, "check_run", checkRun("TypeScript", "abe92a9", "in_progress", ""))
	delivered(t, s, "check_run", checkRun("Browser", "abe92a9", "queued", ""))

	open := listed(t, s)
	require.Len(t, open[0].GetChecks(), 2)
	assert.Equal(t, "running", open[0].GetChecks()[0].GetState())
	assert.Equal(t, "waiting", open[0].GetChecks()[1].GetState())

	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysMergeWhenReady)
	assert.Equal(t, http.StatusConflict, status, "%v", answered)

	delivered(t, s, "check_run", checkRun("TypeScript", "abe92a9", "completed", "success"))
	delivered(t, s, "check_run", checkRun("Browser", "abe92a9", "completed", "failure"))
	open = listed(t, s)
	assert.Equal(t, "done", open[0].GetChecks()[0].GetState())
	assert.Equal(t, "failed", open[0].GetChecks()[1].GetState())
	status, _ = press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusConflict, status)

	// A check on a head nobody waits at is nobody's.
	assert.Empty(t, delivered(t, s, "check_run", checkRun("Browser", "0ther", "completed", "success")))
}

// "I want to be able to change my mind": a push moves the pull request to a
// new head, and the old head's checks and answers are history.
func TestAPushMovesTheApprovalToItsNewHead(t *testing.T) {
	s, _ := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")
	status, _ := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionDontMerge, saysCancel)
	require.Equal(t, http.StatusOK, status)

	delivered(t, s, "pull_request", pullRequest("synchronize", 29, "c0ffee", ""))
	open := listed(t, s)
	require.Len(t, open, 1)
	assert.Equal(t, "c0ffee", open[0].GetSha())
	assert.Empty(t, open[0].GetChecks())
	assert.Empty(t, open[0].GetSaid())

	// A press about the head that moved is refused: what was seen is not what would be merged.
	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusConflict, status, "%v", answered)
}

// "Approvals is human only" "approvals is ROOT only for now": a token is
// refused however ROOT it is, and so is a session below ROOT.
func TestOnlyTheHumanAtROOTAnswers(t *testing.T) {
	s, _ := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")

	token := auth.Admitted(auth.LevelRoot, "garden")
	token.Identity = rootAccount
	token.Grant = &auth.Grant{Label: "ROOT agent", DID: "did:key:z6Mkagent", MintedBy: rootAccount}
	status, answered := press(s, auth.WithAdmission(context.Background(), token), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, answered["error"], "token")

	user := auth.Admitted(auth.LevelUser, "garden")
	user.Identity = "google:1"
	status, _ = press(s, auth.WithAdmission(context.Background(), user), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusForbidden, status)

	status, _ = press(s, context.Background(), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusUnauthorized, status)

	// A press that says what no option says is not a press.
	status, _ = press(s, asRoot(), "teranos/elements#29", "abe92a9", "Yes", "yes")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Empty(t, listed(t, s)[0].GetSaid())
}

// "press again to force merge": the node merges at the head the human saw, and
// writes the merge down by the human.
func TestForceMergeMergesNowByTheHuman(t *testing.T) {
	s, fake := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")

	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysMergeWhenReady)
	require.Equal(t, http.StatusOK, status, "%v", answered)
	assert.Equal(t, didWaiting, answered["did"])
	assert.Empty(t, fake.merges)
	assert.True(t, listed(t, s)[0].GetWaits())

	status, answered = press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	require.Equal(t, http.StatusOK, status, "%v", answered)
	assert.Equal(t, didMerged, answered["did"])
	assert.Equal(t, "m3rg3d", answered["merge_sha"])
	require.Len(t, fake.merges, 1)
	assert.Contains(t, fake.merges[0], `"sha":"abe92a9"`)
	assert.Empty(t, listed(t, s), "merged, it waits on nobody")

	var merged, answeredBy []string
	for _, as := range systemHolds(t, s) {
		if len(as.Predicates) == 0 || as.Subjects[0] != "teranos/elements#29" {
			continue
		}
		switch as.Predicates[0] {
		case watcher.ApprovalMerged:
			merged = as.Actors
		case watcher.ApprovalAnswered:
			answeredBy = as.Actors
		}
	}
	assert.Equal(t, []string{rootAccount}, merged, "the merge is the human's")
	assert.Equal(t, []string{rootAccount}, answeredBy)
}

// "Click 1 means merge after CI passes": it merges when main's CI concludes
// green, and not before.
func TestMergeWaitsOnMainsCI(t *testing.T) {
	s, fake := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")

	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysMergeWhenReady)
	require.Equal(t, http.StatusOK, status, "%v", answered)
	assert.Equal(t, didWaiting, answered["did"])

	// A suite concluding on main while another still runs merges nothing.
	suite := `{"action":"completed","check_suite":{"head_branch":"main","head_sha":"35174ca","status":"completed","conclusion":"success"},"repository":{"full_name":"teranos/elements","default_branch":"main"}}`
	assert.Empty(t, delivered(t, s, "check_suite", suite))
	assert.Empty(t, fake.merges)

	fake.mainGreen = true
	assert.Equal(t, []string{"teranos/elements#29"}, delivered(t, s, "check_suite", suite))
	require.Len(t, fake.merges, 1)
	assert.Empty(t, listed(t, s))

	// Main green already when the human presses: it merges at once.
	delivered(t, s, "pull_request", pullRequest("opened", 32, "beef", ""))
	checksPass(t, s, "beef")
	status, answered = press(s, asRoot(), "teranos/elements#32", "beef", optionMerge, saysMergeWhenReady)
	require.Equal(t, http.StatusOK, status, "%v", answered)
	assert.Equal(t, didMerged, answered["did"])
}

// "If we still wait for CI, the NO will cancel the yes. Press NO again and
// it's definitely NO."
func TestDontMergeCancelsAWaitingMerge(t *testing.T) {
	s, fake := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")

	status, _ := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysMergeWhenReady)
	require.Equal(t, http.StatusOK, status)
	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionDontMerge, saysCancel)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, didNothing, answered["did"])
	assert.False(t, listed(t, s)[0].GetWaits())

	fake.mainGreen = true
	suite := `{"action":"completed","check_suite":{"head_branch":"main","head_sha":"35174ca","status":"completed","conclusion":"success"},"repository":{"full_name":"teranos/elements","default_branch":"main"}}`
	assert.Empty(t, delivered(t, s, "check_suite", suite))
	assert.Empty(t, fake.merges, "cancelled, main going green merges nothing")

	status, _ = press(s, asRoot(), "teranos/elements#29", "abe92a9", optionDontMerge, saysDefinitelyNo)
	require.Equal(t, http.StatusOK, status)
	open := listed(t, s)
	assert.Equal(t, saysDefinitelyNo, open[0].GetSaid())

	// Closed on GitHub, it waits on nobody.
	delivered(t, s, "pull_request", pullRequest("closed", 29, "abe92a9", ""))
	assert.Empty(t, listed(t, s))
}

// GitHub refusing the merge is written down, and the human is told.
func TestARefusedMergeIsSaidAndWrittenDown(t *testing.T) {
	s, _ := approvingServer(t)
	delivered(t, s, "pull_request", pullRequest("opened", 29, "abe92a9", ""))
	checksPass(t, s, "abe92a9")
	s.gitHubService().SetBaseURL("http://127.0.0.1:1")

	status, answered := press(s, asRoot(), "teranos/elements#29", "abe92a9", optionMerge, saysForceMerge)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.NotEmpty(t, answered["error"])
	assert.Len(t, listed(t, s), 1, "not merged, it still waits")
}

// The signum says what it holds, so it is served: as a path, as a tool, and in
// the document.
func TestTheApprovalsSignumIsServed(t *testing.T) {
	s, _ := approvingServer(t)
	signum, err := answeredOf(s.approvalsSignum())
	require.NoError(t, err)
	require.NoError(t, signum.Check())
	holds(t, s.approvalsSignum(), "list", &protocol.Approvals{Approvals: []*protocol.Approval{{Subject: "teranos/elements#29"}}})
}
