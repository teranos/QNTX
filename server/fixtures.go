package server

// What names a person is a fixture before it leaves the node: ground's scanner
// (ground, controls/fixtures.pbt) is run over what the node hands ROOT's agents
// and over what the App's webhook says left for GitHub in words.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
)

// fixturesPredicate is the line the node writes when something left for GitHub
// naming a person, and what the node did about it.
const fixturesPredicate = "fixtures:named"

// scrubber is text with fixtures in place of what names a person, and what was
// found in a line, empty when nothing was. An error is a scanner that did not run.
type scrubber func(ctx context.Context, text string) (scrubbed, found string, err error)

// scrub runs the node's scanner: the one a test stands in, or ground's.
func (s *QNTXServer) scrub(ctx context.Context, text string) (string, string, error) {
	if s.scrubWith != nil {
		return s.scrubWith(ctx, text)
	}
	return s.groundScrub(ctx, text)
}

// groundScrub runs the scrub of a ground this node built for one of its agents.
// A node that built none has no scanner, and says so.
func (s *QNTXServer) groundScrub(ctx context.Context, text string) (string, string, error) {
	var ground string
	for _, home := range s.underGround.all() {
		candidate := comet.AgentGround(home)
		if info, err := os.Stat(candidate); err == nil && info.Mode().Perm()&0o100 != 0 {
			ground = candidate
			break
		}
	}
	if ground == "" {
		return "", "", errors.New("no agent of this node runs under a built ground, so there is no scanner to run")
	}
	cmd := exec.CommandContext(ctx, ground, "scrub")
	cmd.Stdin = strings.NewReader(text)
	var out, said bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &said
	if err := cmd.Run(); err != nil {
		return "", "", errors.Wrapf(err, "%s scrub did not run: %s", ground, strings.TrimSpace(said.String()))
	}
	// A ground from before scrub reads stdin as a hook event and prints nothing:
	// that is no answer, not a clean one.
	if len(text) > 0 && out.Len() == 0 {
		return "", "", errors.Newf("%s scrub answered nothing, so it is a ground from before scrub: the agent's ground has to be built again", ground)
	}
	found := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(said.String()), "ground scrub:"))
	return out.String(), found, nil
}

// rootsAgent is whether the caller is one of ROOT's agents, by its token's
// label: the Node Agent, or an element agent (ADR-048, ADR-054).
func rootsAgent(admitted auth.Admission) bool {
	g := admitted.Grant
	return g != nil && (g.Label == auth.NodeAgentLabel || strings.HasPrefix(g.Label, auth.ElementAgentLabel+":"))
}

// fixtured is a tool's answer as one of ROOT's agents reads it: with fixtures
// in place of what names a person. An answer the scanner could not run over is
// not handed on. Anyone else reads the answer as it is.
func (s *QNTXServer) fixtured(ctx context.Context, admitted auth.Admission, result *mcp.CallToolResult) *mcp.CallToolResult {
	if result == nil || !rootsAgent(admitted) {
		return result
	}
	for i, c := range result.Content {
		text, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		scrubbed, _, err := s.scrub(ctx, text.Text)
		if err != nil {
			return refused("the answer was not handed on: it could not be made into fixtures: %v", err)
		}
		result.Content[i] = &mcp.TextContent{Text: scrubbed}
	}
	if raw, ok := result.StructuredContent.(json.RawMessage); ok && len(raw) > 0 {
		scrubbed, _, err := s.scrub(ctx, string(raw))
		if err != nil {
			return refused("the answer was not handed on: it could not be made into fixtures: %v", err)
		}
		result.StructuredContent = json.RawMessage(scrubbed)
	}
	return result
}

// The parts of GitHub's deliveries that are words somebody wrote.
type gitHubWordsEvent struct {
	Action      string `json:"action"`
	Before      string `json:"before"`
	After       string `json:"after"`
	Deleted     bool   `json:"deleted"`
	Number      int64  `json:"number"`
	PullRequest struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"pull_request"`
	Issue struct {
		Number int64  `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	} `json:"issue"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	} `json:"comment"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// fixturesFromGitHub reads one delivery of the App's webhook for words that
// left for GitHub, puts fixtures where one names a person, and names what it
// touched. A title, a body or a comment is written back as the App.
func (s *QNTXServer) fixturesFromGitHub(ctx context.Context, event string, body []byte) ([]string, error) {
	var e gitHubWordsEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, errors.Wrapf(err, "the %s event is not readable JSON", event)
	}
	owner, repo, named := strings.Cut(e.Repository.FullName, "/")
	if !named {
		return []string{}, nil
	}
	as := services.AsInstallation(ctx)
	switch event {
	case "push":
		if e.Deleted || e.After == "" {
			return []string{}, nil
		}
		return s.pushedWords(as, owner, repo, e.Before, e.After)
	case "pull_request":
		if !wordsMoved(e.Action) {
			return []string{}, nil
		}
		return s.wordsWrittenBack(as, e.Repository.FullName, e.PullRequest.Title+"\n"+e.PullRequest.Body, func(ctx context.Context, title, text string) error {
			answered, err := s.gitHubService().UpdateAPullRequest(ctx, &protocol.GitHubUpdateAPullRequestRequest{Owner: owner, Repo: repo, PullNumber: e.Number, Title: title, Body: text})
			return wrote(answered.GetSuccess(), answered.GetError(), err)
		}, "#"+strconv.FormatInt(e.Number, 10))
	case "issues":
		if !wordsMoved(e.Action) {
			return []string{}, nil
		}
		return s.wordsWrittenBack(as, e.Repository.FullName, e.Issue.Title+"\n"+e.Issue.Body, func(ctx context.Context, title, text string) error {
			answered, err := s.gitHubService().UpdateAnIssue(ctx, &protocol.GitHubUpdateAnIssueRequest{Owner: owner, Repo: repo, IssueNumber: e.Issue.Number, Title: structpb.NewStringValue(title), Body: text})
			return wrote(answered.GetSuccess(), answered.GetError(), err)
		}, "#"+strconv.FormatInt(e.Issue.Number, 10))
	case "issue_comment":
		if !wordsMoved(e.Action) {
			return []string{}, nil
		}
		return s.wordsWrittenBack(as, e.Repository.FullName, "\n"+e.Comment.Body, func(ctx context.Context, _, text string) error {
			answered, err := s.gitHubService().UpdateAnIssueComment(ctx, &protocol.GitHubUpdateAnIssueCommentRequest{Owner: owner, Repo: repo, CommentId: e.Comment.ID, Body: text})
			return wrote(answered.GetSuccess(), answered.GetError(), err)
		}, "#"+strconv.FormatInt(e.Issue.Number, 10)+" comment "+strconv.FormatInt(e.Comment.ID, 10))
	case "pull_request_review_comment":
		if !wordsMoved(e.Action) {
			return []string{}, nil
		}
		return s.wordsWrittenBack(as, e.Repository.FullName, "\n"+e.Comment.Body, func(ctx context.Context, _, text string) error {
			answered, err := s.gitHubService().UpdateAReviewCommentForAPullRequest(ctx, &protocol.GitHubUpdateAReviewCommentForAPullRequestRequest{Owner: owner, Repo: repo, CommentId: e.Comment.ID, Body: text})
			return wrote(answered.GetSuccess(), answered.GetError(), err)
		}, "review comment "+strconv.FormatInt(e.Comment.ID, 10))
	}
	return []string{}, nil
}

// wordsMoved is an action that put words on GitHub or changed them.
func wordsMoved(action string) bool {
	switch action {
	case "opened", "edited", "created", "reopened", "synchronize":
		return true
	}
	return false
}

// wrote is what writing back came to, as one error.
func wrote(success bool, said string, err error) error {
	if err != nil {
		return err
	}
	if !success {
		return errors.New(said)
	}
	return nil
}

// wordsWrittenBack scrubs the title and text of one thing on GitHub and, when
// a fixture took a place, writes both back and writes the line down. The first
// line of words is the title; the rest the body.
func (s *QNTXServer) wordsWrittenBack(ctx context.Context, repo, words string, write func(ctx context.Context, title, text string) error, what string) ([]string, error) {
	scrubbed, found, err := s.scrub(ctx, words)
	if err != nil {
		return nil, errors.Wrapf(err, "%s %s was not checked", repo, what)
	}
	if found == "" {
		return []string{}, nil
	}
	title, text, _ := strings.Cut(scrubbed, "\n")
	if err := write(ctx, title, text); err != nil {
		return nil, errors.Wrapf(err, "%s %s carries %s, and the fixtures were not written back", repo, what, found)
	}
	subject := repo + " " + what
	if err := s.approvalLine(s.nodeActor(), subject, fixturesPredicate, repo, map[string]any{"found": found, "written_back": true}); err != nil {
		return nil, err
	}
	return []string{subject}, nil
}

// pushedWords reads what a push added, line by line, and marks the commit
// failed by the name of each file whose added lines name a person: a commit
// cannot be written back, so it is refused where it stands.
func (s *QNTXServer) pushedWords(ctx context.Context, owner, repo, before, after string) ([]string, error) {
	full := owner + "/" + repo
	var files []*protocol.GitHubCommitsCommitsDiffEntry
	if before == "" || strings.Trim(before, "0") == "" {
		// A branch GitHub has not seen before compares to nothing: its head alone is read.
		got, err := s.gitHubService().GetACommit(ctx, &protocol.GitHubGetACommitRequest{Owner: owner, Repo: repo, Ref: after})
		if err := wrote(got.GetSuccess(), got.GetError(), err); err != nil {
			return nil, errors.Wrapf(err, "the commit %s of %s was not read", after, full)
		}
		files = got.GetFiles()
	} else {
		got, err := s.gitHubService().CompareTwoCommits(ctx, &protocol.GitHubCompareTwoCommitsRequest{Owner: owner, Repo: repo, Basehead: before + "..." + after})
		if err := wrote(got.GetSuccess(), got.GetError(), err); err != nil {
			return nil, errors.Wrapf(err, "%s...%s of %s was not compared", before, after, full)
		}
		files = got.GetFiles()
	}
	named := []string{}
	for _, f := range files {
		_, found, err := s.scrub(ctx, addedLines(f.GetPatch()))
		if err != nil {
			return nil, errors.Wrapf(err, "%s of %s at %s was not checked", f.GetFilename(), full, after)
		}
		if found != "" {
			named = append(named, f.GetFilename()+": "+found)
		}
	}
	if len(named) == 0 {
		return []string{}, nil
	}
	description := strings.Join(named, "; ")
	if len(description) > 140 {
		description = description[:137] + "..."
	}
	answered, err := s.gitHubService().CreateACommitStatus(ctx, &protocol.GitHubCreateACommitStatusRequest{
		Owner: owner, Repo: repo, Sha: after, State: "failure", Context: "fixtures", Description: description,
	})
	if err := wrote(answered.GetSuccess(), answered.GetError(), err); err != nil {
		return nil, errors.Wrapf(err, "%s at %s adds a line that names a person (%s), and the commit was not marked", full, after, description)
	}
	subject := full + "@" + after
	if err := s.approvalLine(s.nodeActor(), subject, fixturesPredicate, full, map[string]any{"found": named, "status": "failure"}); err != nil {
		return nil, err
	}
	return []string{subject}, nil
}

// addedLines is what a patch adds, the lines alone.
func addedLines(patch string) string {
	var added strings.Builder
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added.WriteString(line[1:])
			added.WriteByte('\n')
		}
	}
	return added.String()
}
