package quote

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// fakeGitHub answers the loop's operations from canned JSON and keeps what
// it was asked.
type fakeGitHub struct {
	answers map[string]string
	asked   []string
	sent    map[string]map[string]any
}

func (f *fakeGitHub) ask(operation string, request any) ([]byte, error) {
	f.asked = append(f.asked, operation)
	raw, _ := json.Marshal(request)
	var sent map[string]any
	_ = json.Unmarshal(raw, &sent)
	if f.sent == nil {
		f.sent = map[string]map[string]any{}
	}
	f.sent[operation] = sent
	answer, ok := f.answers[operation]
	if !ok {
		answer = `{}`
	}
	return []byte(answer), nil
}

func greenGitHub(fileText string) *fakeGitHub {
	return &fakeGitHub{answers: map[string]string{
		"ListCheckRunsForAGitReference": `{"check_runs":[{"name":"Go Tests","status":"completed","conclusion":"success"}]}`,
		"ListCommitsOnAPullRequest":     `{"items":[{"commit":{"message":"A change\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"}}]}`,
		"ListPullRequestsFiles":         `{"items":[{"filename":"docs/x.md","status":"modified","patch":"@@ -1,2 +1,3 @@\n-old\n+she said \"Build QNTX here\"\n+and \"words nobody ever typed\"\n context"}]}`,
		"GetABranch":                    `{"name":"main","commit":{"sha":"abc123"}}`,
		"GetRepositoryContent":          `{"content":"` + base64.StdEncoding.EncodeToString([]byte(fileText)) + `","sha":"blob1"}`,
		"CreateAPullRequest":            `{"html_url":"https://github.com/o/r/pull/9"}`,
	}}
}

func landingOf() Landing {
	return Landing{Owner: "o", Repo: "r", DefaultBranch: "main", Number: 8, HeadSha: "head1"}
}

func TestALandingWithAnAgentsUnsourcedQuoteIsCorrectedInAPullRequest(t *testing.T) {
	gh := greenGitHub("she said \"Build QNTX here\"\nand \"words nobody ever typed\"\n")
	done, err := NewLanded(gh.ask, arrivalsWith(true, "Build QNTX here"), zap.NewNop().Sugar()).Correct(landingOf())
	if err != nil {
		t.Fatal(err)
	}
	if done.Skipped != "" || done.PullRequest != "https://github.com/o/r/pull/9" {
		t.Fatalf("done = %+v", done)
	}
	if len(done.Spans) != 1 || done.Spans[0].Span != "words nobody ever typed" {
		t.Errorf("spans = %+v", done.Spans)
	}
	if done.Agents[0] != "Fable 5.1" || done.Branch != "quotes/pr-8" {
		t.Errorf("agents %v branch %s", done.Agents, done.Branch)
	}
	want := []string{"ListCheckRunsForAGitReference", "ListCommitsOnAPullRequest", "ListPullRequestsFiles", "GetABranch", "CreateAReference", "GetRepositoryContent", "CreateOrUpdateFileContents", "CreateAPullRequest"}
	if strings.Join(gh.asked, " ") != strings.Join(want, " ") {
		t.Errorf("asked %v", gh.asked)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	if string(written) != "she said \"Build QNTX here\"\nand 「[Fable 5.1] words nobody ever typed」\n" {
		t.Errorf("corrected file:\n%s", written)
	}
	if gh.sent["CreateOrUpdateFileContents"]["branch"] != "quotes/pr-8" || gh.sent["CreateAReference"]["sha"] != "abc123" {
		t.Errorf("sent %v", gh.sent)
	}
	if !strings.Contains(gh.sent["CreateAPullRequest"]["body"].(string), "words nobody ever typed") {
		t.Errorf("the pull request does not name the span: %v", gh.sent["CreateAPullRequest"]["body"])
	}
}

func TestALandingWithRedChecksIsLeftAlone(t *testing.T) {
	gh := greenGitHub("")
	gh.answers["ListCheckRunsForAGitReference"] = `{"check_runs":[{"name":"Go Tests","status":"completed","conclusion":"failure"}]}`
	done, err := NewLanded(gh.ask, arrivalsWith(true), zap.NewNop().Sugar()).Correct(landingOf())
	if err != nil || !strings.Contains(done.Skipped, "Go Tests") || len(gh.asked) != 1 {
		t.Errorf("done = %+v, err %v, asked %v", done, err, gh.asked)
	}
}

func TestALandingNoAgentAuthoredIsAPersonsOwn(t *testing.T) {
	gh := greenGitHub("")
	gh.answers["ListCommitsOnAPullRequest"] = `{"items":[{"commit":{"message":"A human change"}}]}`
	done, err := NewLanded(gh.ask, arrivalsWith(true), zap.NewNop().Sugar()).Correct(landingOf())
	if err != nil || done.Skipped == "" || len(gh.asked) != 2 {
		t.Errorf("done = %+v, err %v, asked %v", done, err, gh.asked)
	}
}

func TestALandingWhoseQuotesArePeoplesOpensNothing(t *testing.T) {
	gh := greenGitHub("")
	done, err := NewLanded(gh.ask, arrivalsWith(true, "Build QNTX here", "words nobody ever typed"), zap.NewNop().Sugar()).Correct(landingOf())
	if err != nil || done.Skipped == "" || done.PullRequest != "" || len(gh.asked) != 3 {
		t.Errorf("done = %+v, err %v, asked %v", done, err, gh.asked)
	}
}

func TestAgentsOfReadsTheTrailers(t *testing.T) {
	got := AgentsOf([]string{"x\n\nCo-Authored-By: Claude Fable 5.1 <a@b>\nCo-Authored-By: Codex <c@d>", "y\nCo-Authored-By: Claude Fable 5.1 <a@b>"})
	if strings.Join(got, "|") != "Fable 5.1|Codex" {
		t.Errorf("agents = %v", got)
	}
	if n := len(AgentsOf([]string{"no trailer"})); n != 0 {
		t.Errorf("found %d agents in a plain message", n)
	}
}

func TestAddedLinesAreThePlusLines(t *testing.T) {
	got := AddedLines("--- a\n+++ b\n@@ -1 +1,2 @@\n-gone\n+kept\n+also\n same")
	if strings.Join(got, "|") != "kept|also" {
		t.Errorf("added = %v", got)
	}
}
