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
	// Number is the pull request that landed, when the landing is one; a push
	// to the default branch lands with none.
	Number   int64    `json:"number"`
	HeadSha  string   `json:"head_sha"`
	BaseSha  string   `json:"base_sha"`
	Messages []string `json:"messages"`
}

// Name is how the landing is spoken of: #N, or the commit that landed.
func (l Landing) Name() string {
	if l.Number > 0 {
		return "#" + strconv.FormatInt(l.Number, 10)
	}
	if len(l.HeadSha) > 9 {
		return l.HeadSha[:9]
	}
	return l.HeadSha
}

// Asker asks GitHub one operation as the App's installation, as GitHubService
// does, and hands back what GitHub answered as JSON.
type Asker func(operation string, request any) ([]byte, error)

// A Correction is what the loop did with a landing: the pull request it
// opened, or why it opened none.
type Correction struct {
	Landing Landing  `json:"landing"`
	Agents  []string `json:"agents,omitempty"`
	Files   []string `json:"files,omitempty"`
	Lines   int      `json:"lines,omitempty"`
	// Sure is how many lines ground saw an agent write; Likely how many are
	// the agent's only by the commits naming it as co-author.
	Sure        int    `json:"sure,omitempty"`
	Likely      int    `json:"likely,omitempty"`
	Spans       []Span `json:"spans,omitempty"`
	Branch      string `json:"branch,omitempty"`
	PullRequest string `json:"pull_request,omitempty"`
	Skipped     string `json:"skipped,omitempty"`
}

// Landed is the loop: for every pull request that lands on main, the agent's
// words it put inside a person's quotation marks
// are found and put inside the agent's own, in a pull request that follows.
type Landed struct {
	ask     Asker
	measure func(Arrival) (Arrived, error)
	// writes is every write an agent made in the namespace that knows the
	// project, as ground attested them; none where no namespace does.
	writes func(project string) ([]Wrote, error)
	logger *zap.SugaredLogger
}

// NewLanded is the loop, measuring with the arrivals it is given.
func NewLanded(ask Asker, arrivals *Arrivals, writes func(project string) ([]Wrote, error), logger *zap.SugaredLogger) *Landed {
	return &Landed{ask: ask, measure: arrivals.Measure, writes: writes, logger: logger}
}

// Correct reads the landing, measures what it added, and opens the
// correcting pull request when there is something to correct.
func (l *Landed) Correct(landing Landing) (Correction, error) {
	done := Correction{Landing: landing}
	agents, err := l.agentsOf(landing)
	if err != nil {
		return done, err
	}
	done.Agents = agents
	project := landing.Owner + "/" + landing.Repo
	writes, err := l.writes(project)
	if err != nil {
		return done, err
	}
	if len(agents) == 0 && len(writes) == 0 {
		done.Skipped = "no agent is known to have written any of it: no commit names one, and ground saw no agent write here"
		return done, nil
	}
	files, err := l.files(landing)
	if err != nil {
		return done, err
	}
	trailer := strings.Join(agents, ", ")
	marks := map[string][]LineMark{}
	for _, f := range files {
		added := AddedLines(f.Patch)
		if len(added) == 0 {
			continue
		}
		// What the agent quoted in double quotes is measured, so the pull request
		// says which of it no person said; the lines are the agent's regardless.
		arrived, err := l.measure(Arrival{Project: project, Ref: landing.HeadSha, Texts: added})
		if err != nil {
			return done, err
		}
		for _, sp := range arrived.Spans {
			if arrived.Known && sp.Kind == "unsourced" {
				done.Spans = append(done.Spans, sp)
			}
		}
		for _, line := range added {
			if !ProseLine(line) {
				continue
			}
			// For sure: ground saw an agent write the line, and says which model.
			// Failing that, likely: a commit names an agent as co-author.
			label := trailer
			if w, sure := WroteLine(writes, line); sure {
				if label = ModelLabel(w.Model); label == "" {
					label = "agent"
				}
				done.Sure++
			} else if trailer == "" {
				continue
			} else {
				done.Likely++
			}
			marks[f.Filename] = append(marks[f.Filename], LineMark{From: line, To: MarkLine(line, label)})
			done.Lines++
			done.Files = appendOnce(done.Files, f.Filename)
		}
	}
	if len(marks) == 0 {
		done.Skipped = "it added no prose outside the agent's quotation"
		return done, nil
	}
	done.Branch = "quotes/" + strings.TrimPrefix(landing.Name(), "#")
	if landing.Number > 0 {
		done.Branch = "quotes/pr-" + strconv.FormatInt(landing.Number, 10)
	}
	if err := l.branch(landing, done.Branch); err != nil {
		return done, err
	}
	for _, file := range done.Files {
		if err := l.correctFile(landing, done.Branch, file, marks[file], trailer); err != nil {
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
	if landing.Number == 0 {
		return AgentsOf(landing.Messages), nil
	}
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
	operation, request := "ListPullRequestsFiles", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "pull_number": landing.Number}
	if landing.Number == 0 {
		// A push's files are what changed between what the branch was and what it is.
		operation, request = "CompareTwoCommits", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "basehead": landing.BaseSha + "..." + landing.HeadSha}
	}
	err := l.pages(operation, request,
		func(raw []byte) (int, error) {
			var answered struct {
				Items []changedFile `json:"items"`
				Files []changedFile `json:"files"`
			}
			if err := json.Unmarshal(raw, &answered); err != nil {
				return 0, errors.Wrap(err, "the files did not read")
			}
			answered.Items = append(answered.Items, answered.Files...)
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

// correctFile puts the agent's lines of one file inside the agent's own
// quotation, as a commit on the correcting branch.
func (l *Landed) correctFile(landing Landing, branch, path string, marks []LineMark, label string) error {
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
	corrected, n := Marked(string(text), marks)
	if n == 0 {
		return nil
	}
	message := "Lines of " + labelOr(label) + " in " + path + " stand in the agent's own quotation\n\n" +
		strconv.Itoa(n) + " line(s) an agent wrote, outside its quotation when " + landing.Name() + " landed (ADR-055)."
	_, err = l.ask("CreateOrUpdateFileContents", map[string]any{"owner": landing.Owner, "repo": landing.Repo, "path": path,
		"message": message, "content": base64.StdEncoding.EncodeToString([]byte(corrected)), "sha": answered.Sha, "branch": branch})
	return err
}

// pullRequest opens the correcting pull request and is its link.
func (l *Landed) pullRequest(landing Landing, done Correction) (string, error) {
	var body strings.Builder
	n := landing.Name()
	body.WriteString(n + " landed on " + landing.DefaultBranch + ". An agent wrote these lines, and they stood outside the agent's quotation: " + strconv.Itoa(done.Sure) + " of them ground saw the agent write, in its session and by its model, which is for sure; " + strconv.Itoa(done.Likely) + " are the agent's by the commits naming " + trailerOr(done.Agents) + " as co-author. They stand inside it now, as ADR-055 settles: an agent's words inside 「 」 with the agent's name first, carrying their content and no authority. A person's words quoted inside them keep their double quotes.\n\n")
	for _, file := range done.Files {
		body.WriteString("- " + file + "\n")
	}
	if len(done.Spans) > 0 {
		body.WriteString("\nQuoted inside them in double quotes, and no person said it, measured against every prompt the namespace holds:\n\n")
		for _, sp := range done.Spans {
			body.WriteString("- " + sp.Span + " (" + strconv.Itoa(sp.Corrections) + " corrections over " + strconv.Itoa(sp.Length) + ")\n")
		}
	}
	raw, err := l.ask("CreateAPullRequest", map[string]any{"owner": landing.Owner, "repo": landing.Repo,
		"title": "What " + strings.Join(done.Agents, ", ") + " wrote in " + n + " stands in the agent's own quotation",
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

// A LineMark is one line an agent wrote as it landed, and as it stands inside
// the agent's quotation.
type LineMark struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProseLine is whether an added line is words an agent wrote that stand
// outside its quotation: not blank, not already marked, not code.
func ProseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "", strings.Contains(line, "「"), strings.HasPrefix(trimmed, "```"),
		strings.HasPrefix(line, "    "), strings.HasPrefix(line, "\t"), strings.HasPrefix(trimmed, "|"):
		return false
	}
	_, text := markdownPrefix(line)
	return strings.ContainsFunc(text, func(r rune) bool { return r > ' ' && !strings.ContainsRune("-=*_#>|", r) })
}

// markdownPrefix is the structure a line keeps outside the quotation, a
// heading mark, a list mark, a block quote mark, and the words after it.
func markdownPrefix(line string) (string, string) {
	rest := strings.TrimLeft(line, " ")
	prefix := line[:len(line)-len(rest)]
	for _, mark := range []string{"- [ ] ", "- [x] ", "- ", "* ", "+ ", "> "} {
		if strings.HasPrefix(rest, mark) {
			return prefix + mark, rest[len(mark):]
		}
	}
	if strings.HasPrefix(rest, "#") {
		n := len(rest) - len(strings.TrimLeft(rest, "#"))
		if n <= 6 && len(rest) > n && rest[n] == ' ' {
			return prefix + rest[:n+1], rest[n+1:]
		}
	}
	digits := len(rest) - len(strings.TrimLeft(rest, "0123456789"))
	if digits > 0 && len(rest) > digits+1 && rest[digits] == '.' && rest[digits+1] == ' ' {
		return prefix + rest[:digits+2], rest[digits+2:]
	}
	return prefix, rest
}

// MarkLine is the line inside the agent's quotation, its structure kept
// outside: 「[Fable 5.1] the words」.
func MarkLine(line, label string) string {
	prefix, text := markdownPrefix(line)
	return prefix + "「[" + label + "] " + strings.TrimRight(text, " ") + "」"
}

// Marked is text with every marked line standing as its mark says, and how
// many lines moved.
func Marked(text string, marks []LineMark) (string, int) {
	lines := strings.Split(text, "\n")
	n := 0
	for _, mark := range marks {
		for i, line := range lines {
			if line == mark.From {
				lines[i] = mark.To
				n++
			}
		}
	}
	return strings.Join(lines, "\n"), n
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

func trailerOr(agents []string) string {
	if len(agents) == 0 {
		return "no one"
	}
	return strings.Join(agents, ", ")
}

func labelOr(label string) string {
	if label == "" {
		return "an agent"
	}
	return label
}
