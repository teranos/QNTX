package quote

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// LandedHandlerName is the built-in a merged pull request reaches.
const LandedHandlerName = "quote.landed"

// The numbers the loop reads GitHub with, so none is read out of the code below.
const (
	// pageSize is how many a GitHub list is asked for at once: the most it gives.
	pageSize = 100

	// pageCeiling is how many pages of one list are read before the loop stops
	// and says so, so an answer that ignores the page it was asked for cannot
	// keep it reading.
	pageCeiling = 50
)

// A Landing is a pull request that landed on its repository's default branch:
// what the loop reads, measures, and answers with a pull request of its own.
type Landing struct {
	Owner         string `json:"owner"`
	Repo          string `json:"repo"`
	DefaultBranch string `json:"default_branch"`
	Number        int64  `json:"number"`
	HeadSha       string `json:"head_sha"`
}

// Asker asks GitHub one operation as the App's installation, as GitHubService
// does, and hands back what GitHub answered as JSON.
type Asker func(operation string, request any) ([]byte, error)

// A Correction is what the loop did with a landing: the pull request it
// opened, or why it opened none.
type Correction struct {
	Landing     Landing  `json:"landing"`
	Agents      []string `json:"agents,omitempty"`
	Files       []string `json:"files,omitempty"`
	Spans       []Span   `json:"spans,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	PullRequest string   `json:"pull_request,omitempty"`
	Skipped     string   `json:"skipped,omitempty"`
}

// Landed is the loop: for every pull request that lands on main with its
// checks green, the agent's words it put inside a person's quotation marks
// are found and put inside the agent's own, in a pull request that follows.
type Landed struct {
	ask     Asker
	measure func(Arrival) (Arrived, error)
	logger  *zap.SugaredLogger
}

// NewLanded is the loop, measuring with the arrivals it is given.
func NewLanded(ask Asker, arrivals *Arrivals, logger *zap.SugaredLogger) *Landed {
	return &Landed{ask: ask, measure: arrivals.Measure, logger: logger}
}

// Correct reads the landing, measures what it added, and opens the
// correcting pull request when there is something to correct.
func (l *Landed) Correct(landing Landing) (Correction, error) {
	done := Correction{Landing: landing}
	ok, why, err := l.checksGreen(landing)
	if err != nil {
		return done, err
	}
	if !ok {
		done.Skipped = why
		return done, nil
	}
	agents, err := l.agentsOf(landing)
	if err != nil {
		return done, err
	}
	if len(agents) == 0 {
		done.Skipped = "no agent authored a commit of it, so its quotes are a person's own"
		return done, nil
	}
	done.Agents = agents
	files, err := l.files(landing)
	if err != nil {
		return done, err
	}
	unsourced := map[string][]string{}
	for _, f := range files {
		added := AddedLines(f.Patch)
		if len(added) == 0 {
			continue
		}
		arrived, err := l.measure(Arrival{Project: landing.Owner + "/" + landing.Repo, Ref: landing.HeadSha, Texts: added})
		if err != nil {
			return done, err
		}
		// A project the node holds no namespace for has no prompts to read: its
		// spans are unmeasured, not unsourced, and none is the agent's to claim.
		if len(arrived.Spans) > 0 && !arrived.Known {
			done.Spans, done.Files = nil, nil
			done.Skipped = "the node holds no namespace for " + landing.Owner + "/" + landing.Repo + ", so none of its quotes is known to be a person's or an agent's"
			return done, nil
		}
		for _, s := range arrived.Spans {
			if s.Kind != "unsourced" {
				continue
			}
			unsourced[f.Filename] = append(unsourced[f.Filename], s.Span)
			done.Spans = append(done.Spans, s)
			done.Files = appendOnce(done.Files, f.Filename)
		}
	}
	if len(unsourced) == 0 {
		done.Skipped = "every quoted span it added is a person's words"
		return done, nil
	}
	label := strings.Join(agents, ", ")
	done.Branch = "quotes/pr-" + strconv.FormatInt(landing.Number, 10)
	if err := l.branch(landing, done.Branch); err != nil {
		return done, err
	}
	for _, file := range done.Files {
		if err := l.correctFile(landing, done.Branch, file, unsourced[file], label); err != nil {
			return done, err
		}
	}
	url, err := l.pullRequest(landing, done)
	if err != nil {
		return done, err
	}
	done.PullRequest = url
	return done, nil
}

// checksGreen is whether every check run on the landing's head completed and
// passed, and when not, why the landing is left alone.
func (l *Landed) checksGreen(landing Landing) (bool, string, error) {
	type checkRun struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	var runs []checkRun
	err := l.pages("ListCheckRunsForAGitReference", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "ref": landing.HeadSha},
		func(raw []byte) (int, error) {
			var answered struct {
				CheckRuns []checkRun `json:"check_runs"`
			}
			if err := json.Unmarshal(raw, &answered); err != nil {
				return 0, errors.Wrap(err, "the check runs did not read")
			}
			runs = append(runs, answered.CheckRuns...)
			return len(answered.CheckRuns), nil
		})
	if err != nil {
		return false, "", err
	}
	if len(runs) == 0 {
		return false, "no check ran on it", nil
	}
	for _, run := range runs {
		if run.Status != "completed" || (run.Conclusion != "success" && run.Conclusion != "neutral" && run.Conclusion != "skipped") {
			return false, "check " + run.Name + " is " + run.Status + " " + run.Conclusion, nil
		}
	}
	return true, "", nil
}

// pages reads a list GitHub answers a page at a time, handing each page to
// read, which says how many it held, until a page comes back short.
func (l *Landed) pages(operation string, request map[string]any, read func(raw []byte) (int, error)) error {
	for page := 1; page <= pageCeiling; page++ {
		request["per_page"], request["page"] = pageSize, page
		raw, err := l.ask(operation, request)
		if err != nil {
			return errors.Wrapf(err, "%s, page %d", operation, page)
		}
		held, err := read(raw)
		if err != nil {
			return err
		}
		if held < pageSize {
			return nil
		}
	}
	return errors.Newf("%s held more than %d pages of %d, so what it holds past them was not read", operation, pageCeiling, pageSize)
}

// agentsOf is the agents the pull request's commits name as co-authors.
func (l *Landed) agentsOf(landing Landing) ([]string, error) {
	var messages []string
	err := l.pages("ListCommitsOnAPullRequest", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "pull_number": landing.Number},
		func(raw []byte) (int, error) {
			var answered struct {
				Items []struct {
					Commit struct {
						Message string `json:"message"`
					} `json:"commit"`
				} `json:"items"`
			}
			if err := json.Unmarshal(raw, &answered); err != nil {
				return 0, errors.Wrap(err, "the commits did not read")
			}
			for _, item := range answered.Items {
				messages = append(messages, item.Commit.Message)
			}
			return len(answered.Items), nil
		})
	if err != nil {
		return nil, err
	}
	return AgentsOf(messages), nil
}

type changedFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
}

// files is every file the pull request changed, with its patch.
func (l *Landed) files(landing Landing) ([]changedFile, error) {
	var kept []changedFile
	err := l.pages("ListPullRequestsFiles", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "pull_number": landing.Number},
		func(raw []byte) (int, error) {
			var answered struct {
				Items []changedFile `json:"items"`
			}
			if err := json.Unmarshal(raw, &answered); err != nil {
				return 0, errors.Wrap(err, "the files did not read")
			}
			for _, f := range answered.Items {
				if f.Status != "removed" && Prose(f.Filename) {
					kept = append(kept, f)
				}
			}
			return len(answered.Items), nil
		})
	if err != nil {
		return nil, err
	}
	return kept, nil
}

// Prose is whether a file is words and not code: only there is a span in
// double quotes a claim about what a person said. Source code comments are
// cleaned up by QNTX at some point, and not yet.
func Prose(path string) bool {
	for _, ext := range []string{".md", ".markdown", ".txt", ".adoc", ".rst"} {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// branch makes the correcting branch at the default branch's head.
func (l *Landed) branch(landing Landing, branch string) error {
	raw, err := l.ask("GetABranch", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "branch": landing.DefaultBranch})
	if err != nil {
		return err
	}
	var answered struct {
		Commit struct {
			Sha string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(raw, &answered); err != nil || answered.Commit.Sha == "" {
		return errors.Newf("the head of %s did not read", landing.DefaultBranch)
	}
	_, err = l.ask("CreateAReference", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "ref": "refs/heads/" + branch, "sha": answered.Commit.Sha})
	return err
}

// correctFile puts the agent's words of one file inside the agent's own
// quotation, as a commit on the correcting branch.
func (l *Landed) correctFile(landing Landing, branch, path string, spans []string, label string) error {
	raw, err := l.ask("GetRepositoryContent", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "path": path, "ref": landing.DefaultBranch})
	if err != nil {
		return err
	}
	var answered struct {
		Content string `json:"content"`
		Sha     string `json:"sha"`
	}
	if err := json.Unmarshal(raw, &answered); err != nil {
		return errors.Wrapf(err, "%s did not read", path)
	}
	text, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(answered.Content, "\n", ""))
	if err != nil {
		return errors.Wrapf(err, "%s is not base64 as GitHub gives it", path)
	}
	corrected, n := Corrected(string(text), spans, label)
	if n == 0 {
		return nil
	}
	message := "Quotes of " + label + " in " + path + " stand in the agent's own quotation\n\n" +
		strconv.Itoa(n) + " span(s) no person said, found when #" + strconv.FormatInt(landing.Number, 10) + " landed (ADR-055)."
	_, err = l.ask("CreateOrUpdateFileContents", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "path": path,
		"message": message, "content": base64.StdEncoding.EncodeToString([]byte(corrected)), "sha": answered.Sha, "branch": branch})
	return err
}

// pullRequest opens the correcting pull request and is its link.
func (l *Landed) pullRequest(landing Landing, done Correction) (string, error) {
	var body strings.Builder
	body.WriteString("#" + strconv.FormatInt(landing.Number, 10) + " landed with its checks green. These spans stood inside a person's quotation marks, and no person said them: measured against every prompt the namespace holds, each is as far from any as it is long. They are the agent's words, " + strings.Join(done.Agents, ", ") + ", and stand now inside the agent's own quotation (ADR-055).\n\n")
	for _, s := range done.Spans {
		body.WriteString("- " + s.Span + " (" + strconv.Itoa(s.Corrections) + " corrections over " + strconv.Itoa(s.Length) + ")\n")
	}
	raw, err := l.ask("CreateAPullRequest", map[string]any{"owner": landing.Owner, "repo": landing.Repo,
		"title": "Quotes of " + strings.Join(done.Agents, ", ") + " from #" + strconv.FormatInt(landing.Number, 10) + " stand in the agent's own quotation",
		"head":  done.Branch, "base": landing.DefaultBranch, "body": body.String()})
	if err != nil {
		return "", err
	}
	var answered struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &answered); err != nil {
		return "", errors.Wrap(err, "the pull request did not read")
	}
	return answered.HTMLURL, nil
}

// AddedLines is the lines a unified patch adds, without their leading plus.
func AddedLines(patch string) []string {
	var added []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, line[1:])
		}
	}
	return added
}

// AgentsOf is the agents commit messages name as co-authors, each once, in
// order: a Co-Authored-By trailer naming a Claude model, Codex or another
// agent, without the vendor's prefix.
func AgentsOf(messages []string) []string {
	var agents []string
	for _, message := range messages {
		for _, line := range strings.Split(message, "\n") {
			line = strings.TrimSpace(line)
			name, found := strings.CutPrefix(line, "Co-Authored-By:")
			if !found {
				continue
			}
			name = strings.TrimSpace(name)
			if at := strings.Index(name, "<"); at >= 0 {
				name = strings.TrimSpace(name[:at])
			}
			name = strings.TrimPrefix(name, "Claude ")
			if name != "" {
				agents = appendOnce(agents, name)
			}
		}
	}
	return agents
}

// Corrected is text with each span that stood inside double quotes standing
// inside the agent's quotation instead, and how many it moved.
func Corrected(text string, spans []string, label string) (string, int) {
	n := 0
	for _, span := range spans {
		for _, marks := range [][2]string{{`"`, `"`}, {"“", "”"}} {
			from := marks[0] + span + marks[1]
			if count := strings.Count(text, from); count > 0 {
				text = strings.ReplaceAll(text, from, "「["+label+"] "+span+"」")
				n += count
			}
		}
	}
	return text, n
}

func appendOnce(all []string, one string) []string {
	for _, have := range all {
		if have == one {
			return all
		}
	}
	return append(all, one)
}
