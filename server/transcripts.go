package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Transcripts are loom's job, done by the node.
// "I want to kill it, and make sure QNTX takes over"

// Derived from what Ground already streams into the namespace the caller stands
// in, and nothing is written to make them.

// transcriptPredicates is every hook event a transcript reads. Each is asked
// for on its own: a store's filter may AND predicates rather than OR them.
var transcriptPredicates = []string{
	"UserPromptSubmit", "Stop", "PreToolUse",
	"SessionStart", "SessionEnd", "PreCompact",
	"SubagentStart", "SubagentStop", "TaskCompleted",
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
	At      string `json:"at"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	Of      string `json:"of"`

	when time.Time
}

// transcript is one session, every turn in the order it happened.
type transcript struct {
	Session  string           `json:"session"`
	Subjects []string         `json:"subjects"`
	Started  string           `json:"started"`
	Ended    string           `json:"ended"`
	Turns    []transcriptTurn `json:"turns"`
	// Events the store folded into sigmas (ADR-020): said, because no turn
	// can be read back from a sigma.
	Folded int `json:"folded"`
}

func (s *QNTXServer) transcriptsSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "transcripts",
			Description: "What was said and done in each agent session Ground recorded, read from the namespace the caller stands in.",
			Sigils: []*protocol.Sigil{
				{
					Name: "read",
					Does: "Every session Ground recorded here, newest first, as turns: what the person asked, what the agent answered, the tools it reached for, what Ground's controls said, and what a rite found.",
					Takes: []*protocol.Param{
						{Name: "session", Says: "One session, by its id."},
						{Name: "limit", Kind: sigil.Count, Says: "How many sessions, newest first."},
					},
					Gives: []*protocol.Field{
						{Name: "transcripts", Says: "Each session: its id, the subjects it was about, when it started and ended, and its turns, each naming the attestation it was read from."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/transcripts"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"read": s.transcriptsRead},
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
	var contexts []string
	if sent["session"] != "" {
		contexts = []string{"session:" + sent["session"]}
	}
	var events []*types.As
	for _, predicate := range transcriptPredicates {
		found, err := store.GetAttestations(ats.AttestationFilter{Predicates: []string{predicate}, Contexts: contexts, Limit: maxTranscriptRead})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "could not read " + predicate + ": " + err.Error()}
		}
		events = append(events, found...)
	}
	return map[string]any{"transcripts": transcriptsOf(events, limit)}, nil
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
		session := sessionOf(as.Contexts)
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
	case "UserPromptSubmit":
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

// toolTurn is a tool call as loom labelled it.
func toolTurn(tool, path, command string) (string, string) {
	switch tool {
	case "Bash":
		return "tool", command
	case "Edit", "MultiEdit", "NotebookEdit":
		return "edit", path
	case "Write":
		return "write", path
	case "Read":
		return "read", path
	case "Grep", "Glob":
		return "search", tool
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
