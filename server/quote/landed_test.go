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
	// paged answers an operation a page at a time, by the page it was asked for.
	paged map[string][]string
	asked []string
	sent  map[string]map[string]any
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
	if pages, paged := f.paged[operation]; paged {
		page, _ := sent["page"].(float64)
		answer = `{}`
		if n := int(page); n >= 1 && n <= len(pages) {
			answer = pages[n-1]
		}
	}
	return []byte(answer), nil
}

const changedProse = `{"filename":"docs/x.md","status":"modified","patch":"@@ -1,2 +1,3 @@\n-old\n+she said \"Build QNTX here\"\n+and \"words nobody ever typed\"\n context"},{"filename":"ts/x.ts","status":"added","patch":"+const k = \"not a quote of anyone\";"}`

// A code file whose comments an agent wrote: the comment lines are words, the
// code is not, and a directive is not.
const changedCode = `{"filename":"ts/stories.ts","status":"modified","patch":"@@ -1,3 +1,6 @@\n+            // No size and no scroller of its own: whether the stories open as a window or a panel,\n+            // \"You didn't see how the list doesn't use the screenspace available?\"\n+            const k = 1;\n+//go:build qntxquote\n+            // 「[Fable 5.1] already marked」"}`

func greenGitHub(fileText string) *fakeGitHub {
	return &fakeGitHub{answers: map[string]string{
		"ListCommitsOnAPullRequest": `{"items":[{"commit":{"message":"A change\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"}}]}`,
		"ListPullRequestsFiles":     `{"items":[` + changedProse + `]}`,
		"CompareTwoCommits":         `{"files":[` + changedProse + `]}`,
		"GetABranch":                `{"name":"main","commit":{"sha":"abc123"}}`,
		"GetRepositoryContent":      `{"content":"` + base64.StdEncoding.EncodeToString([]byte(fileText)) + `","sha":"blob1"}`,
		"CreateAPullRequest":        `{"html_url":"https://github.com/o/r/pull/9"}`,
	}}
}

func landingOf() Landing {
	return Landing{Owner: "o", Repo: "r", DefaultBranch: "main", Number: 8, HeadSha: "head1"}
}

// pushOf is a push to main with no pull request: its agents are read from its
// own commits, its files from the compare.
func pushOf() Landing {
	return Landing{Owner: "o", Repo: "r", DefaultBranch: "main", HeadSha: "abcdef0123456789", BaseSha: "base0",
		Messages: []string{"A change\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"}}
}

func noWrites(string) ([]Wrote, error) { return nil, nil }

const twoLines = "she said \"Build QNTX here\"\nand \"words nobody ever typed\"\n"

func TestWhatAnAgentWroteInALandingStandsInItsQuotation(t *testing.T) {
	gh := greenGitHub(twoLines)
	done, err := NewLanded(gh.ask, arrivalsWith(true, "Build QNTX here"), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil {
		t.Fatal(err)
	}
	if done.Skipped != "" || done.PullRequest != "https://github.com/o/r/pull/9" || done.Lines != 2 {
		t.Fatalf("done = %+v", done)
	}
	if len(done.Spans) != 1 || done.Spans[0].Span != "words nobody ever typed" {
		t.Errorf("spans no person said = %+v", done.Spans)
	}
	if done.Agents[0] != "Fable 5.1" || done.Branch != "quotes/pr-8" {
		t.Errorf("agents %v branch %s", done.Agents, done.Branch)
	}
	want := []string{"ListCommitsOnAPullRequest", "ListPullRequestsFiles", "GetABranch", "CreateAReference", "GetRepositoryContent", "CreateOrUpdateFileContents", "CreateAPullRequest"}
	if strings.Join(gh.asked, " ") != strings.Join(want, " ") {
		t.Errorf("asked %v", gh.asked)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	if string(written) != "「[Fable 5.1] she said \"Build QNTX here\"」\n「[Fable 5.1] and \"words nobody ever typed\"」\n" {
		t.Errorf("corrected file:\n%s", written)
	}
	if gh.sent["CreateOrUpdateFileContents"]["branch"] != "quotes/pr-8" || gh.sent["CreateAReference"]["sha"] != "abc123" {
		t.Errorf("sent %v", gh.sent)
	}
	body := gh.sent["CreateAPullRequest"]["body"].(string)
	if !strings.Contains(body, "docs/x.md") || !strings.Contains(body, "words nobody ever typed") || !strings.Contains(body, "#8") {
		t.Errorf("the pull request does not say what moved: %v", body)
	}
}

// A push straight to main lands the same way: agents from its own commits,
// files from the compare, and the branch named by the commit.
func TestAPushToMainLandsTheSameWay(t *testing.T) {
	gh := greenGitHub(twoLines)
	done, err := NewLanded(gh.ask, arrivalsWith(true, "Build QNTX here"), noWrites, zap.NewNop().Sugar()).Explicit(pushOf())
	if err != nil || done.Skipped != "" || done.Lines != 2 || done.Branch != "quotes/abcdef012" {
		t.Fatalf("done = %+v, err %v", done, err)
	}
	if gh.asked[0] != "CompareTwoCommits" || gh.sent["CompareTwoCommits"]["basehead"] != "base0...abcdef0123456789" {
		t.Errorf("asked %v with %v", gh.asked, gh.sent["CompareTwoCommits"])
	}
}

// A line already inside the agent's quotation, a code fence, a table row and
// an indented code line are not prose outside it; structure stays outside.
func TestProseLinesAndTheirMarks(t *testing.T) {
	for line, want := range map[string]bool{"plain words": true, "## A heading": true, "- a list item": true, "「[Fable 5.1] marked」": false, "```go": false, "    indented code": false, "| a | table |": false, "": false, "---": false, `"a person's quote, on its own line"`: false, `- "and in a list".`: false, `she said "this" and more`: true} {
		if ProseLine(line) != want {
			t.Errorf("ProseLine(%q) = %v", line, !want)
		}
	}
	for line, want := range map[string]string{
		"## A heading":   "## 「[Fable 5.1] A heading」",
		"- a list item":  "- 「[Fable 5.1] a list item」",
		"  2. numbered":  "  2. 「[Fable 5.1] numbered」",
		"> quoted block": "> 「[Fable 5.1] quoted block」",
		"plain words  ":  "「[Fable 5.1] plain words」",
	} {
		if got := MarkLine(line, "Fable 5.1"); got != want {
			t.Errorf("MarkLine(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestALandingNoAgentAuthoredIsAPersonsOwn(t *testing.T) {
	gh := greenGitHub("")
	gh.answers["ListCommitsOnAPullRequest"] = `{"items":[{"commit":{"message":"A human change"}}]}`
	done, err := NewLanded(gh.ask, arrivalsWith(true), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped == "" || len(gh.asked) != 1 {
		t.Errorf("done = %+v, err %v, asked %v", done, err, gh.asked)
	}
}

// No commit names an agent, and ground saw one write the second line: that
// line is the agent's for sure, named by the session's model, and the first
// line, which no agent is known to have written, stays as it is.
func TestALineGroundSawAnAgentWriteIsTheAgentsForSure(t *testing.T) {
	gh := greenGitHub(twoLines)
	gh.answers["ListCommitsOnAPullRequest"] = `{"items":[{"commit":{"message":"A human change"}}]}`
	writes := func(string) ([]Wrote, error) {
		return []Wrote{{Text: "intro\nand \"words nobody ever typed\"\noutro", File: "docs/x.md", Session: "sess-9", Model: "claude-opus-5-5", Row: "row-9"}}, nil
	}
	done, err := NewLanded(gh.ask, arrivalsWith(true, "Build QNTX here"), writes, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped != "" || done.Sure != 1 || done.Likely != 0 || done.Lines != 1 {
		t.Fatalf("done = %+v, err %v", done, err)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	if string(written) != "she said \"Build QNTX here\"\n「[Opus 5.5] and \"words nobody ever typed\"」\n" {
		t.Errorf("corrected file:\n%s", written)
	}
}

func TestModelLabelIsTheNotationsName(t *testing.T) {
	for model, want := range map[string]string{"claude-fable-5-1": "Fable 5.1", "claude-opus-5-5": "Opus 5.5", "claude-sonnet-5": "Sonnet 5", "codex": "codex", "": ""} {
		if got := ModelLabel(model); got != want {
			t.Errorf("ModelLabel(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestALandingThatAddsNoProseOutsideTheQuotationOpensNothing(t *testing.T) {
	gh := greenGitHub("")
	gh.answers["ListPullRequestsFiles"] = `{"items":[{"filename":"docs/x.md","status":"modified","patch":"+「[Fable 5.1] already marked」\n+\n+` + "```" + `\n+    code\n"}]}`
	done, err := NewLanded(gh.ask, arrivalsWith(true), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped == "" || done.PullRequest != "" || len(gh.asked) != 2 {
		t.Errorf("done = %+v, err %v, asked %v", done, err, gh.asked)
	}
}

// A project the node holds no namespace for still lands: who wrote the lines
// is the landing's to say, and only which quotes a person said goes unmeasured.
func TestAProjectTheNodeDoesNotKnowIsStillMarked(t *testing.T) {
	gh := greenGitHub(twoLines)
	done, err := NewLanded(gh.ask, arrivalsWith(false), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped != "" || done.PullRequest == "" || done.Lines != 2 || len(done.Spans) != 0 {
		t.Fatalf("done = %+v, err %v", done, err)
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

// A string literal in source code is nobody's words: only prose is read.
func TestOnlyProseIsReadForQuotes(t *testing.T) {
	for path, want := range map[string]bool{"docs/adr/ADR-055.md": true, "README.txt": true, "ts/x.ts": false, "server/quote/landed.go": false, "crates/ats/src/quote.rs": false} {
		if Prose(path) != want {
			t.Errorf("Prose(%q) = %v", path, !want)
		}
	}
}

// listOf is a GitHub list answer holding n copies of item under key.
func listOf(key, item string, n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = item
	}
	return `{"` + key + `":[` + strings.Join(items, ",") + `]}`
}

// A co-author on a commit past the first page is still named.
func TestAnAgentOnALaterPagesCommitIsNamed(t *testing.T) {
	gh := greenGitHub("")
	gh.paged = map[string][]string{"ListCommitsOnAPullRequest": {
		listOf("items", `{"commit":{"message":"A human change"}}`, pageSize),
		`{"items":[{"commit":{"message":"Late\n\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"}}]}`,
	}}
	agents, err := NewLanded(gh.ask, arrivalsWith(true), noWrites, zap.NewNop().Sugar()).agentsOf(landingOf())
	if err != nil || strings.Join(agents, "|") != "Fable 5.1" {
		t.Errorf("agents = %v, err %v", agents, err)
	}
}

// A prose file past the first page of files is still read.
func TestAProseFileOnALaterPageIsRead(t *testing.T) {
	gh := greenGitHub("")
	gh.paged = map[string][]string{"ListPullRequestsFiles": {
		listOf("items", `{"filename":"ts/x.ts","status":"added","patch":"+x"}`, pageSize),
		`{"items":[{"filename":"docs/late.md","status":"added","patch":"+\"late\""}]}`,
	}}
	files, err := NewLanded(gh.ask, arrivalsWith(true), noWrites, zap.NewNop().Sugar()).files(landingOf())
	if err != nil || len(files) != pageSize+1 || files[len(files)-1].Filename != "docs/late.md" {
		t.Errorf("%d files, err %v", len(files), err)
	}
}

// An answer that ignores the page it was asked for does not keep the loop
// reading: it stops at the ceiling and says so.
func TestAListThatNeverEndsIsStoppedAndSaid(t *testing.T) {
	gh := greenGitHub("")
	gh.answers["ListCommitsOnAPullRequest"] = listOf("items", `{"commit":{"message":"x"}}`, pageSize)
	_, err := NewLanded(gh.ask, arrivalsWith(true), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("err = %v", err)
	}
	if len(gh.asked) != pageCeiling {
		t.Errorf("asked %d times, want %d", len(gh.asked), pageCeiling)
	}
}

// An agent's prose inside a source comment is agent prose too, and stands
// inside the agent's quotation with the comment marker kept outside; the
// person's quote inside it keeps its double quotes; code and directives stay.
func TestAnAgentsCommentInCodeStandsInItsQuotation(t *testing.T) {
	gh := greenGitHub("            // No size and no scroller of its own: whether the stories open as a window or a panel,\n            // \"You didn't see how the list doesn't use the screenspace available?\"\n            const k = 1;\n//go:build qntxquote\n            // 「[Fable 5.1] already marked」\n")
	gh.answers["ListPullRequestsFiles"] = `{"items":[` + changedCode + `]}`
	done, err := NewLanded(gh.ask, arrivalsWith(true, "You didn't see how the list doesn't use the screenspace available?"), noWrites, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped != "" || done.Lines != 1 {
		t.Fatalf("done = %+v, err %v", done, err)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	want := "            // 「[Fable 5.1] No size and no scroller of its own: whether the stories open as a window or a panel,」\n            // \"You didn't see how the list doesn't use the screenspace available?\"\n            const k = 1;\n//go:build qntxquote\n            // 「[Fable 5.1] already marked」\n"
	if string(written) != want {
		t.Errorf("corrected file:\n%s\nwant:\n%s", written, want)
	}
}

// A person quoting the agent is seen: the span inside the person's double
// quotes that is the agent's own words stands inside the agent's quotation
// within the person's marks, and a person's own quote is left as it is.
func TestAPersonQuotingTheAgentIsSeen(t *testing.T) {
	gh := greenGitHub("the operator wrote: \"Understood: the rule has no exception for comments.\" and also \"my own words\"\n")
	gh.answers["ListPullRequestsFiles"] = `{"items":[{"filename":"docs/x.md","status":"modified","patch":"+the operator wrote: \"Understood: the rule has no exception for comments.\" and also \"my own words\""}]}`
	gh.answers["ListCommitsOnAPullRequest"] = `{"items":[{"commit":{"message":"A human change"}}]}`
	agent := func(string) ([]Wrote, error) {
		return []Wrote{{Text: "Understood: the rule has no exception for comments. An agent's prose inside a source comment is agent prose.", Session: "sess-3", Model: "claude-fable-5-1", Row: "stop-3"}}, nil
	}
	done, err := NewLanded(gh.ask, arrivalsWith(true, "my own words"), agent, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped != "" || done.Quoted != 1 || done.Lines != 1 || done.Sure != 0 || done.Likely != 0 {
		t.Fatalf("done = %+v, err %v", done, err)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	want := "the operator wrote: \"「[Fable 5.1] Understood: the rule has no exception for comments.」\" and also \"my own words\"\n"
	if string(written) != want {
		t.Errorf("corrected file:\n%s\nwant:\n%s", written, want)
	}
}

// A person's quote that holds a sentence of the agent's among their own words
// has that sentence, and only it, inside the agent's quotation.
func TestAPersonQuotingPartOfTheAgentIsSeenInPart(t *testing.T) {
	line := `I wrote in my notes: "as I see it, Understood: the rule has no exception for comments, which I like"`
	agent := []Wrote{{Text: "Understood: the rule has no exception for comments. More follows.", Model: "claude-fable-5-1"}}
	got, n := AgentQuoted(agent, line)
	want := `I wrote in my notes: "as I see it, 「[Fable 5.1] Understood: the rule has no exception for comments」, which I like"`
	if n != 1 || got != want {
		t.Errorf("got %d\n%s\nwant\n%s", n, got, want)
	}
	if got, n := AgentQuoted(agent, `she said "nothing of the agent's here"`); n != 0 || got != `she said "nothing of the agent's here"` {
		t.Errorf("a person's own quote was touched: %q", got)
	}
}

// A model that writes another model's name over its own words is caught:
// ground saw the session that wrote the line, and its model, and the name is
// put right and the pass-off counted.
func TestAModelPassingOffAsAnotherIsNamedRight(t *testing.T) {
	gh := greenGitHub("「[Fable 5.1] a line a lesser model wrote under the better name」\n")
	gh.answers["ListPullRequestsFiles"] = `{"items":[{"filename":"docs/x.md","status":"modified","patch":"+「[Fable 5.1] a line a lesser model wrote under the better name」"}]}`
	agent := func(string) ([]Wrote, error) {
		return []Wrote{{Text: "「[Fable 5.1] a line a lesser model wrote under the better name」", File: "docs/x.md", Session: "sess-5", Model: "claude-sonnet-5-5"}}, nil
	}
	done, err := NewLanded(gh.ask, arrivalsWith(true), agent, zap.NewNop().Sugar()).Explicit(landingOf())
	if err != nil || done.Skipped != "" || done.PassedOff != 1 || done.Lines != 1 {
		t.Fatalf("done = %+v, err %v", done, err)
	}
	written, _ := base64.StdEncoding.DecodeString(gh.sent["CreateOrUpdateFileContents"]["content"].(string))
	if string(written) != "「[Sonnet 5.5] a line a lesser model wrote under the better name」\n" {
		t.Errorf("corrected file:\n%s", written)
	}
	if !strings.Contains(gh.sent["CreateAPullRequest"]["body"].(string), "1 line(s) stood under another model's name") {
		t.Errorf("the pull request does not name the pass-off: %v", gh.sent["CreateAPullRequest"]["body"])
	}
}

func TestCodeThatIsNotACommentIsLeftAlone(t *testing.T) {
	for _, line := range []string{
		"\t*p = 1;",
		"*p;",
		"// +build linux",
		"//   +build linux",
		"//go:build linux",
	} {
		path := "x.c"
		if ProseLineIn(path, line) {
			t.Errorf("%q is read as prose", line)
		}
		if got := MarkLineIn(path, line, "agent"); got != line {
			t.Errorf("%q was rewritten to %q", line, got)
		}
	}
	if got := MarkLineIn("x.c", " * says something", "agent"); !strings.Contains(got, "「[agent] says something」") {
		t.Errorf("a block comment's line lost its quotation: %q", got)
	}
}
