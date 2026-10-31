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

// Explicitness is what the loop did with a landing: the pull request that
// made the provenance of its words explicit, or why it opened none.
type Explicitness struct {
	Landing Landing  `json:"landing"`
	Agents  []string `json:"agents,omitempty"`
	Files   []string `json:"files,omitempty"`
	Lines   int      `json:"lines,omitempty"`
	// Sure is how many lines ground saw an agent write; Likely how many are
	// the agent's only by the commits naming it as co-author.
	Sure   int `json:"sure,omitempty"`
	Likely int `json:"likely,omitempty"`
	// Quoted is how many spans a person quoted in double quotes were an
	// agent's own words, now inside the agent's quotation within the person's.
	Quoted int `json:"quoted,omitempty"`
	// PassedOff is how many lines stood under another model's name than the one
	// ground saw write them, and carry the right name now.
	PassedOff   int    `json:\"passed_off,omitempty\"`
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

// Explicit reads the landing and opens the pull request that makes the
// provenance of its words explicit, when any of them lacks it. Not a check:
// nothing passes or fails, every word is said to be whose it is.
func (l *Landed) Explicit(landing Landing) (Explicitness, error) {
	done := Explicitness{Landing: landing}
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
		done.Skipped = "no agent is known to have written or said any of it: no commit names one, and ground saw no agent here"
		return done, nil
	}
	files, err := l.files(landing)
	if err != nil {
		return done, err
	}
	trailer := strings.Join(agents, ", ")
	marks := map[string][]LineMark{}
	for _, f := range files {
		var added []string
		for _, line := range AddedLines(f.Patch) {
			_, _, marked := markOf(line)
			if ProseLineIn(f.Filename, line) || QuoteLineIn(f.Filename, line) || marked {
				added = append(added, line)
			}
		}
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
			// A span a person put in double quotes that is an agent's own words,
			// said or written in the namespace, stands inside the agent's quotation
			// within the person's: the person quoted the agent, and it is seen.
			to := line
			// A line already under a model's name is held to ground: when the
			// session that wrote it ran another model, the name is put right.
			if label, text, marked := markOf(line); marked {
				if w, sure := WroteLine(writes, text); sure && labelOf(w) != label {
					to = strings.Replace(line, "「["+label+"] ", "「["+labelOf(w)+"] ", 1)
					done.PassedOff++
				}
			}
			if ProseLineIn(f.Filename, line) {
				// For sure: ground saw an agent write the line, and says which model.
				// Failing that, likely: a commit names an agent as co-author.
				label := trailer
				if w, sure := WroteLine(writes, line); sure {
					if label = ModelLabel(w.Model); label == "" {
						label = "agent"
					}
					done.Sure++
					to = MarkLineIn(f.Filename, line, label)
				} else if trailer != "" {
					done.Likely++
					to = MarkLineIn(f.Filename, line, label)
				}
			}
			if to == line {
				// The line is a person's. A span the person put in double quotes that
				// is an agent's own words, or holds a sentence of them, has that part
				// inside the agent's quotation within the person's: quoting the agent is seen.
				var quoted int
				to, quoted = AgentQuoted(writes, line)
				done.Quoted += quoted
			}
			if to == line {
				continue
			}
			marks[f.Filename] = append(marks[f.Filename], LineMark{From: line, To: to})
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
		if err := l.explicitFile(landing, done.Branch, file, marks[file], trailer); err != nil {
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
				if f.Status != "removed" && (Prose(f.Filename) || commentMarkers(f.Filename) != nil) {
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

// Prose is whether a file is words and not code. In a code file only the
// comment lines are words, and they are read too (commentMarkers).
func Prose(path string) bool {
	for _, ext := range []string{".md", ".markdown", ".txt", ".adoc", ".rst"} {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// branch makes the branch at the default branch's head.
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

// explicitFile puts the agent's lines of one file inside the agent's own
// quotation, as a commit on the branch.
func (l *Landed) explicitFile(landing Landing, branch, path string, marks []LineMark, label string) error {
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

// pullRequest opens the pull request that makes it explicit and is its link.
func (l *Landed) pullRequest(landing Landing, done Explicitness) (string, error) {
	var body strings.Builder
	n := landing.Name()
	body.WriteString(n + " landed on " + landing.DefaultBranch + ". " + strconv.Itoa(done.PassedOff) + " line(s) stood under another model's name than the one ground saw write them, and carry the right name now. " + strconv.Itoa(done.Quoted) + " span(s) a person quoted were an agent's own words, and stand inside the agent's quotation within the person's. An agent wrote these lines, and they stood outside the agent's quotation: " + strconv.Itoa(done.Sure) + " of them ground saw the agent write, in its session and by its model, which is for sure; " + strconv.Itoa(done.Likely) + " are the agent's by the commits naming " + trailerOr(done.Agents) + " as co-author. They stand inside it now, as ADR-055 settles: an agent's words inside 「 」 with the agent's name first, carrying their content and no authority. A person's words quoted inside them keep their double quotes.\n\n")
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
	if quoteOnly(text) {
		return false
	}
	return strings.ContainsFunc(text, func(r rune) bool { return r > ' ' && !strings.ContainsRune("-=*_#>|", r) })
}

// quoteOnly is whether words are one span in double quotes and nothing else,
// a closing mark of punctuation allowed: a person's quote standing on its own
// line, as every quote in ADR-055 does. It is never the agent's prose.
func quoteOnly(text string) bool {
	t := strings.TrimRight(strings.TrimSpace(text), ".,;:")
	if len(t) < 2 {
		return false
	}
	open := strings.HasPrefix(t, `"`) || strings.HasPrefix(t, "“")
	close := strings.HasSuffix(t, `"`) || strings.HasSuffix(t, "”")
	if !open || !close {
		return false
	}
	inner := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(t, `"`), "“"), `"`), "”")
	return inner != "" && !strings.ContainsAny(inner, `"“”`)
}

// QuoteLineIn is whether a line, of a file of its kind, is only a person's
// quote: read for measurement, never marked as the agent's.
func QuoteLineIn(path, line string) bool {
	if Prose(path) {
		_, text := markdownPrefix(line)
		return quoteOnly(text)
	}
	if markers := commentMarkers(path); markers != nil {
		rest := strings.TrimLeft(line, " \t")
		for _, marker := range markers {
			if after, found := strings.CutPrefix(rest, marker); found {
				return quoteOnly(after)
			}
		}
	}
	return false
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

// commentMarkers is how a code file begins a line of words, by its kind; nil
// for a file whose words are not behind a marker, or that is not read at all.
func commentMarkers(path string) []string {
	lower := strings.ToLower(path)
	for _, kind := range []struct {
		exts    []string
		markers []string
	}{
		{[]string{".go", ".rs", ".ts", ".tsx", ".js", ".jsx", ".d", ".c", ".h", ".cpp", ".java", ".kt", ".swift", ".proto", ".pbt"}, []string{"///", "//!", "//", "*"}},
		{[]string{".py", ".sh", ".fish", ".nix", ".toml", ".yml", ".yaml", ".rb", ".mk", "makefile", "dockerfile"}, []string{"#"}},
		{[]string{".sql", ".lua"}, []string{"--"}},
	} {
		for _, ext := range kind.exts {
			if strings.HasSuffix(lower, ext) {
				return kind.markers
			}
		}
	}
	return nil
}

// commentPrefix is the indent and marker a comment line keeps outside the
// quotation, and the words after it. A directive such as go:build, a line that
// closes a block, and a line of no words are not prose.
func commentPrefix(markers []string, line string) (string, string, bool) {
	rest := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(rest)]
	for _, marker := range markers {
		if !strings.HasPrefix(rest, marker) {
			continue
		}
		after := rest[len(marker):]
		if marker == "*" && (strings.HasPrefix(after, "/") || strings.HasPrefix(after, "*")) {
			return "", "", false
		}
		if strings.HasPrefix(after, "go:") || strings.HasPrefix(after, "nolint") || strings.HasPrefix(after, "+build") {
			return "", "", false
		}
		text := strings.TrimLeft(after, " ")
		if quoteOnly(text) || !strings.ContainsFunc(text, func(r rune) bool { return r > ' ' && !strings.ContainsRune("-=*_#>|/", r) }) {
			return "", "", false
		}
		return indent + marker + after[:len(after)-len(text)], text, true
	}
	return "", "", false
}

// ProseLineIn is ProseLine for a file of its kind: a prose file's lines, or
// a code file's comment lines.
func ProseLineIn(path, line string) bool {
	if Prose(path) {
		return ProseLine(line)
	}
	markers := commentMarkers(path)
	if markers == nil || strings.Contains(line, "「") {
		return false
	}
	_, _, ok := commentPrefix(markers, line)
	return ok
}

// MarkLineIn is MarkLine for a file of its kind: in a code file the comment
// marker stays outside the quotation, as a heading mark does in prose.
func MarkLineIn(path, line, label string) string {
	if Prose(path) {
		return MarkLine(line, label)
	}
	prefix, text, ok := commentPrefix(commentMarkers(path), line)
	if !ok {
		return line
	}
	return prefix + "「[" + label + "] " + strings.TrimRight(text, " ") + "」"
}

// AgentQuoted is the line with each double-quoted span that is an agent's own
// words, or holds a sentence of them, standing inside the agent's quotation
// within the person's marks, and how many spans that touched.
func AgentQuoted(agent []Wrote, line string) (string, int) {
	n := 0
	for _, span := range Spans(line) {
		marked, label := span, ""
		if w, theirs := WroteLine(agent, span); theirs {
			label = labelOf(w)
			marked = "「[" + label + "] " + span + "」"
		} else if w, sentence := agentSentenceIn(agent, span); sentence != "" {
			label = labelOf(w)
			marked = strings.Replace(span, sentence, "「["+label+"] "+sentence+"」", 1)
		}
		if marked == span {
			continue
		}
		for _, marks := range [][2]string{{`"`, `"`}, {"“", "”"}} {
			from := marks[0] + span + marks[1]
			if strings.Contains(line, from) {
				line = strings.ReplaceAll(line, from, marks[0]+marked+marks[1])
				n++
			}
		}
	}
	return line, n
}

// sentenceLeast is the shortest run of an agent's words inside a person's
// quote that counts as the agent's: shorter, and it is a phrase anyone says.
const sentenceLeast = 24

// agentSentenceIn is the longest sentence of an agent's that a span holds, at
// least sentenceLeast long, and whose it is.
func agentSentenceIn(agent []Wrote, span string) (Wrote, string) {
	var best Wrote
	longest := ""
	for _, w := range agent {
		for _, sentence := range strings.FieldsFunc(w.Text, func(r rune) bool { return r == '.' || r == '!' || r == '?' || r == '\n' }) {
			sentence = strings.TrimSpace(sentence)
			if len(sentence) >= sentenceLeast && len(sentence) > len(longest) && strings.Contains(span, sentence) {
				best, longest = w, sentence
			}
		}
	}
	return best, longest
}

func labelOf(w Wrote) string {
	if label := ModelLabel(w.Model); label != "" {
		return label
	}
	return "agent"
}

// markOf is the name and the words of a line that already stands inside an
// agent's quotation, 「[Name] words」, when it does.
func markOf(line string) (string, string, bool) {
	start := strings.Index(line, "「[")
	if start < 0 {
		return "", "", false
	}
	rest := line[start+len("「["):]
	end := strings.Index(rest, "] ")
	if end < 0 {
		return "", "", false
	}
	label := rest[:end]
	words := rest[end+len("] "):]
	if close := strings.Index(words, "」"); close >= 0 {
		words = words[:close]
	}
	if label == "" || strings.TrimSpace(words) == "" {
		return "", "", false
	}
	return label, words, true
}
