package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	quotes "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/quote"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
)

// Transcripts are loom's job, done by the node.
// "I want to kill it, and make sure QNTX takes over"

// Derived from what Ground already streams into the namespace the caller stands
// in, and nothing is written to make them.

// What this cannot read: a session Ground never streamed, or streamed only in
// part. Loom read those from the files Claude Code writes under
// ~/.claude/projects (qntx-plugins/loom/lib/jsonl_reader.ml at d512ffb2).

// transcriptPredicates are the hook events a session is found by. Each is asked
// for on its own: a store's filter may AND predicates rather than OR them.
var transcriptPredicates = []string{
	"UserPromptSubmit", "QueuedPromptSubmit", "Stop", "PreToolUse",
	"SessionStart", "SessionEnd", "PreCompact",
	"SubagentStart", "SubagentStop", "TaskCompleted", "StopFailure",
	"GroundedUserPromptSubmit", "GroundedPreToolUse", "GroundedPostToolUse", "GroundedStop",
	"ritual:rite",
}

// How many events of one kind a read covers, and how many sessions an answer
// holds when the caller names no limit.
const (
	maxTranscriptRead      = 5000
	transcriptSessionLimit = 20
)

// transcriptTurn is one thing said or done in a session: who, what, when, and
// the attestation it was read from.
type transcriptTurn struct {
	At      string
	Speaker string
	Text    string
	Of      string

	when time.Time
}

// transcript is one session, every turn in the order it happened, as it is
// read; what is answered is protocol.Transcript.
type transcript struct {
	Session  string
	Subjects []string
	Started  string
	Ended    string
	Turns    []transcriptTurn
	// Events the store folded into sigmas (ADR-020): said, because no turn
	// can be read back from a sigma.
	Folded int
	// The model its SessionStart names, and the effort.level its last Stop ran at.
	Model  string
	Effort string

	effortAt time.Time
}

// message is the session as it is answered.
func (t transcript) message() *protocol.Transcript {
	turns := make([]*protocol.Turn, 0, len(t.Turns))
	for _, turn := range t.Turns {
		turns = append(turns, &protocol.Turn{At: turn.At, Speaker: turn.Speaker, Text: turn.Text, Of: turn.Of})
	}
	return &protocol.Transcript{Session: t.Session, Subjects: t.Subjects, Started: t.Started, Ended: t.Ended,
		Turns: turns, Folded: uint32(t.Folded), Model: t.Model, Effort: t.Effort}
}

func (s *QNTXServer) transcriptsSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "transcripts",
			Description: "What was said and done in each agent session Ground recorded, read from the namespace the caller stands in.",
			Tags:        []string{"transcripts", "agent", "session", "quote"},
			Follows:     []*protocol.Follows{transcriptsFollowTEI()},
			Sigils: []*protocol.Sigil{
				{
					Name: "read",
					Does: "Every session Ground recorded here, newest first, as turns: what the person asked, what the agent answered, the tools it reached for, what Ground's controls said, what a rite found, and an API error in the words it came in.",
					Takes: []*protocol.Param{
						{Name: "session", Says: "One session, by its id."},
						{Name: "limit", Kind: sigil.Count, Says: "How many sessions, newest first."},
					},
					Answer: "protocol.Transcripts",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/transcripts"},
				},
				{
					Name: "quoted",
					Does: "The spans in double quotes of a text, each measured against what people said here, as TEI quotes: the words, whose they are, the turn they came from, and how many corrections away. A span inside an agent's quotation is the agent's and is not one.",
					Takes: []*protocol.Param{
						{Name: "text", Required: true, Says: "The words whose quoted spans are measured: a commit message, a PR description, a note."},
					},
					Answer: "protocol.Quotes",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/transcripts/quoted"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"read": s.transcriptsRead, "quoted": s.transcriptsQuoted},
	}
}

func (s *QNTXServer) transcriptsRead(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "a transcript is read from where its caller stands, and this asking carried no request"}
	}
	store, err := s.storeFor(caller)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no namespace to read transcripts from: " + err.Error()}
	}
	limit := transcriptSessionLimit
	if sent["limit"] != "" {
		limit, err = strconv.Atoi(sent["limit"])
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "limit", Says: "limit is a whole number, and " + sent["limit"] + " is not"}
		}
	}
	sessions := []string{sent["session"]}
	if sent["session"] == "" {
		var events []*types.As
		for _, predicate := range transcriptPredicates {
			found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{predicate}, Limit: maxTranscriptRead})
			if err != nil {
				return nil, &protocol.Refusal{Why: sigil.Failed, Says: "could not read " + predicate + ": " + err.Error()}
			}
			events = append(events, found...)
		}
		sessions = sessions[:0]
		for _, t := range transcriptsOf(events, limit) {
			sessions = append(sessions, t.Session)
		}
	}
	read, refused := sessionsIn(store, sessions, limit)
	if refused != nil {
		return nil, refused
	}
	answer := &protocol.Transcripts{Transcripts: make([]*protocol.Transcript, 0, len(read))}
	for _, t := range read {
		answer.Transcripts = append(answer.Transcripts, t.message())
	}
	return answer, nil
}

// humanPredicates are the events a transcript reads as the person speaking.
var humanPredicates = []string{"UserPromptSubmit", "QueuedPromptSubmit"}

// maxHumanRead is a read of every human turn a namespace holds. Zero would be
// none of them.
const maxHumanRead = 1_000_000

// humanTurnsIn is everything the person said in the store: the human turns of
// its transcripts, as typed. A store that will not answer is an error, not no
// turns: the two read the same, and one of them calls every quote invented.
// humanSaidIn is every prompt a namespace holds with the row it is in: who
// typed it, when, in which session. What quote measures spans against.
func humanSaidIn(store namespaces.Reading) ([]quote.Said, error) {
	var said []quote.Said
	for _, predicate := range humanPredicates {
		found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{predicate}, Limit: maxHumanRead})
		if err != nil {
			return nil, errors.Wrapf(err, "reading %s", predicate)
		}
		for _, as := range found {
			text, _ := as.Attributes["prompt"].(string)
			if text == "" {
				continue
			}
			// A prompt outside any session is no one's turn, as transcriptsOf reads them.
			session := statusline.SessionOf(as.Contexts)
			if session == "" {
				continue
			}
			s := quote.Said{Text: text, Row: as.ID, At: as.Timestamp.UTC().Format(time.RFC3339), Session: session}
			if len(as.Actors) > 0 {
				s.Who = as.Actors[0]
			}
			said = append(said, s)
		}
	}
	return said, nil
}

func humanTurnsIn(store namespaces.Reading) ([]string, error) {
	var events []*types.As
	for _, predicate := range humanPredicates {
		found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{predicate}, Limit: maxHumanRead})
		if err != nil {
			return nil, errors.Wrapf(err, "reading %s", predicate)
		}
		events = append(events, found...)
	}
	var said []string
	for _, t := range transcriptsOf(events, len(events)) {
		for _, turn := range t.Turns {
			if turn.Speaker == "human" && turn.Text != "" {
				said = append(said, turn.Text)
			}
		}
	}
	return said, nil
}

// sessionsIn reads each session whole, by its context. Ground names a control's
// row Grounded and the event, for any event, so no list of predicates holds them all.
func sessionsIn(store namespaces.Reading, sessions []string, limit int) ([]transcript, *protocol.Refusal) {
	var events []*types.As
	for _, session := range sessions {
		found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Contexts: []string{"session:" + session}, Limit: maxTranscriptRead})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "could not read session " + session + ": " + err.Error()}
		}
		events = append(events, found...)
	}
	return transcriptsOf(events, limit), nil
}

// transcriptsOf is the events as sessions, newest first, the first limit of them.
func transcriptsOf(events []*types.As, limit int) []transcript {
	bySession := map[string]*transcript{}
	subjects := map[string]map[string]bool{}
	// The store's predicate filter is broader than equality: asking for
	// PreToolUse also answers GroundedPreToolUse. One attestation is one turn.
	read := map[string]bool{}
	for _, as := range events {
		if read[as.ID] {
			continue
		}
		read[as.ID] = true
		session := statusline.SessionOf(as.Contexts)
		if session == "" {
			continue
		}
		folded := as.Source == "distill"
		turn, ok := turnOf(as)
		if !folded && !ok {
			continue
		}
		t, seen := bySession[session]
		if !seen {
			t = &transcript{Session: session, Subjects: []string{}, Turns: []transcriptTurn{}}
			bySession[session] = t
			subjects[session] = map[string]bool{}
		}
		if folded {
			t.Folded += foldedCount(as.Attributes)
			t.Started = earlier(t.Started, seenAt(as.Attributes, "_first_seen"))
			t.Ended = later(t.Ended, seenAt(as.Attributes, "_last_seen"))
			continue
		}
		t.Turns = append(t.Turns, turn)
		ranOn(t, as)
		for _, subject := range as.Subjects {
			if !subjects[session][subject] {
				subjects[session][subject] = true
				t.Subjects = append(t.Subjects, subject)
			}
		}
	}
	out := make([]transcript, 0, len(bySession))
	for _, t := range bySession {
		if len(t.Turns) == 0 {
			// Everything it had was folded: it is said by its count alone.
			out = append(out, *t)
			continue
		}
		sort.SliceStable(t.Turns, func(i, j int) bool {
			if !t.Turns[i].when.Equal(t.Turns[j].when) {
				return t.Turns[i].when.Before(t.Turns[j].when)
			}
			return t.Turns[i].Of < t.Turns[j].Of
		})
		t.Started = earlier(t.Started, t.Turns[0].At)
		t.Ended = later(t.Ended, t.Turns[len(t.Turns)-1].At)
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ended != out[j].Ended {
			return out[i].Ended > out[j].Ended
		}
		return out[i].Session < out[j].Session
	})
	if limit < len(out) {
		out = out[:limit]
	}
	return out
}

// ranOn takes the model off a SessionStart and the effort off the latest Stop.
func ranOn(t *transcript, as *types.As) {
	switch as.Predicates[0] {
	case "SessionStart":
		if model := attrString(as.Attributes, "model"); model != "" {
			t.Model = model
		}
	case "Stop":
		effort, ok := as.Attributes["effort"].(map[string]any)
		if !ok {
			return
		}
		level := attrString(effort, "level")
		if level != "" && !as.Timestamp.Before(t.effortAt) {
			t.Effort, t.effortAt = level, as.Timestamp
		}
	}
}

// turnOf reads one event as a turn, with loom's speakers. A search names its
// tool: its pattern stays on the machine that ran it, and is not made up here.
func turnOf(as *types.As) (transcriptTurn, bool) {
	if len(as.Predicates) == 0 {
		return transcriptTurn{}, false
	}
	turn := transcriptTurn{At: as.Timestamp.UTC().Format(time.RFC3339), Of: as.ID, when: as.Timestamp}
	predicate := as.Predicates[0]
	attr := func(key string) string { return attrString(as.Attributes, key) }
	switch predicate {
	case "UserPromptSubmit", "QueuedPromptSubmit":
		// A message typed while a turn ran fires no hook; ground reads it from
		// Claude Code's own file (ground source/queued.d). The person said it.
		turn.Speaker, turn.Text = "human", attr("prompt")
	case "Stop":
		turn.Speaker, turn.Text = "assistant", attr("last_assistant_message")
	case "PreToolUse":
		turn.Speaker, turn.Text = toolTurn(attr("tool_name"), attr("file_path"), attr("command"))
	case "SessionStart", "SessionEnd":
		turn.Speaker, turn.Text = "session", strings.TrimPrefix(predicate, "Session")+" "+attr("source")
	case "PreCompact":
		turn.Speaker, turn.Text = "compaction", "compacted"
	case "SubagentStart", "SubagentStop":
		turn.Speaker, turn.Text = "agent", strings.TrimPrefix(predicate, "Subagent")+" "+attr("agent_type")
	case "TaskCompleted":
		turn.Speaker, turn.Text = "task", attr("task_subject")
	case "StopFailure":
		// "the mic needs to speak its exact error"
		// The API's error ended the turn. It is said in the words it came in:
		// which error, then what Claude Code wrote of it, whole.
		turn.Speaker, turn.Text = "error", attr("error")+": "+attr("last_assistant_message")
	case "ritual:rite":
		turn.Speaker, turn.Text = "rite", riteSaid(as.Attributes)
	default:
		event, grounded := strings.CutPrefix(predicate, "Grounded")
		if !grounded {
			return transcriptTurn{}, false
		}
		turn.Speaker, turn.Text = "ground", attr("control")+" on "+event
	}
	turn.Text = strings.TrimSpace(turn.Text)
	return turn, true
}

// toolTurn is a tool call as loom labelled it. Claude Code names its tools
// Bash and Read, and Pi bash and read: they are one tool to a transcript.
func toolTurn(tool, path, command string) (string, string) {
	switch strings.ToLower(tool) {
	case "bash":
		return "tool", command
	case "edit", "multiedit", "notebookedit":
		return "edit", path
	case "write":
		return "write", path
	case "read":
		return "read", path
	case "grep", "glob", "find", "ls":
		return "search", tool
	}
	if strings.HasPrefix(tool, "mcp__") {
		return "mcp", tool
	}
	return "tool", tool
}

// seenAt is a sigma's first or last seen, as a turn's At is written, or empty.
func seenAt(attrs map[string]any, key string) string {
	at, err := time.Parse(time.RFC3339, attrString(attrs, key))
	if err != nil {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// earlier and later pick between two At times; empty is no time at all.
func earlier(a, b string) string {
	if a == "" || (b != "" && b < a) {
		return b
	}
	return a
}

func later(a, b string) string {
	if b > a {
		return b
	}
	return a
}

// foldedCount is how many events a sigma holds: its _total, or 1.
func foldedCount(attrs map[string]any) int {
	if total, ok := attrs["_total"].(float64); ok && total > 0 {
		return int(total)
	}
	return 1
}

// riteSaid is what a rite found: its name, its verdict and the code it read.
func riteSaid(attrs map[string]any) string {
	said := attrString(attrs, "rite") + " " + attrString(attrs, "verdict")
	if code, ok := attrs["code"].(float64); ok {
		said += " " + strconv.Itoa(int(code))
	}
	return said
}

// transcriptsFollowTEI is how a transcript's turns and a Quote are TEI's q, and
// an Interp its interp: the words as the element's content, the speaker as who,
// the row as source. Transcripts are quotes a person said.
func transcriptsFollowTEI() *protocol.Follows {
	pairs := [][2]string{
		{"protocol.Turn.text", "q.content"},
		{"protocol.Turn.speaker", "q.who"},
		{"protocol.Turn.of", "q.source"},
		{"protocol.Quote.content", "q.content"},
		{"protocol.Quote.who", "q.who"},
		{"protocol.Quote.type", "q.type"},
		{"protocol.Quote.source", "q.source"},
		{"protocol.Quote.resp", "q.resp"},
		{"protocol.Quote.cert", "q.cert"},
		{"protocol.Quote.rend", "q.rend"},
		{"protocol.Quote.ana", "q.ana"},
		{"protocol.Quote.id", "q.xml:id"},
		{"protocol.Interp.content", "interp.content"},
		{"protocol.Interp.resp", "interp.resp"},
		{"protocol.Interp.cert", "interp.cert"},
		{"protocol.Interp.type", "interp.type"},
		{"protocol.Interp.inst", "interp.inst"},
		{"protocol.Interp.id", "interp.xml:id"},
	}
	follows := &protocol.Follows{Reference: "tei"}
	for _, pair := range pairs {
		follows.Columns = append(follows.Columns, &protocol.Corresponds{Field: pair[0], Column: pair[1]})
	}
	return follows
}

// transcriptsQuoted is transcripts quoted: the spans in double quotes of a
// text, each measured against what people said in the namespace the caller
// stands in, and answered as Quotes naming the turn each came from.
func (s *QNTXServer) transcriptsQuoted(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "quotes are measured where their caller stands, and this asking carried no request"}
	}
	if !quotes.Carried {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "quotes are measured in Rust, which this build leaves out: it was built without qntxquote"}
	}
	if sent["text"] == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "text", Says: "text is the words whose quoted spans are measured"}
	}
	spans := quote.Spans(sent["text"])
	answer := &protocol.Quotes{}
	if len(spans) == 0 {
		return answer, nil
	}
	store, err := s.storeFor(caller)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no namespace to measure quotes in: " + err.Error()}
	}
	said, err := humanSaidIn(store)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the prompts did not read: " + err.Error()}
	}
	measured, err := quotes.Measure(spans, quote.Texts(said))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for i, span := range spans {
		m := measured[i]
		q := &protocol.Quote{Content: span, Type: "written", Rend: []string{`"`}, Cert: strconv.Itoa(m.Corrections)}
		// Only a span the policy takes as the person's words names whose and from
		// which turn; the nearest prompt to an unsourced one is no source.
		if near, ok := quote.SaidAt(said, m); ok && quote.Near(m) {
			q.Who = []string{near.Who}
			q.Source = []string{near.Row}
		}
		answer.Quotes = append(answer.Quotes, q)
	}
	return answer, nil
}
