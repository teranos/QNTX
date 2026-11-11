// 「[Opus 5.5] Package acp runs an Agent Client Protocol agent as a harness, through libACP's client side.」
//
// 「[Opus 5.5] "can't we build the client into libACP and make QNTX use it as an optional subsystem? Just like how I」
// 「[Opus 5.5] would like any harness to be pluggable"」
//
// 「[Opus 5.5] A harness that speaks ACP is a binary and its arguments: Run starts it, signs in, starts or continues」
// 「[Opus 5.5] its session, sets the session's options, says one thing, and hands each update the agent sends to the」
// 「[Opus 5.5] caller. Every message is written and read by libACP (github.com/teranos/ground, libacp/), linked with」
// 「[Opus 5.5] the build tag libacp; without it Run refuses.」
package acp

import (
	"strconv"
	"time"

	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] Agent is the ACP agent's process: its binary, arguments, directory and environment, whole.」
type Agent struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string
}

// 「[Opus 5.5] Turn is one thing said to the agent in a session.」
type Turn struct {
	// 「[Opus 5.5] Cwd is the session's working directory, absolute.」
	Cwd string
	// 「[Opus 5.5] Session is the agent's session to continue; empty starts one.」
	Session string
	// 「[Opus 5.5] Auth is the authMethods id signed in with before any session; empty signs in with none.」
	Auth string
	// 「[Opus 5.5] Config is each session config option and the value id it is set to, in order, before the prompt.」
	Config [][2]string
	Says   string
	// 「[Opus 5.5] Permission chooses the option a session/request_permission is answered with, "" cancelling the」
	// 「[Opus 5.5] turn. Nil answers with a reject option the agent offered, and cancels when it offered none.」
	Permission func(Asked) string
	// 「[Opus 5.5] Client and Version are who the client says it is.」
	Client, Version string
}

// 「[Opus 5.5] UpdateKind is what an update is, as the client reads it.」
type UpdateKind int

const (
	// 「[Opus 5.5] Started is the session the turn runs in, started or continued, before anything else.」
	Started UpdateKind = iota
	Other
	UserMessage
	AgentMessage
	AgentThought
	ToolCall
	ToolCallUpdate
	Usage
)

// 「[Opus 5.5] Update is one session/update, or the Started that leads them. A field the agent did not send is empty.」
type Update struct {
	Kind    UpdateKind
	Session string
	// 「[Opus 5.5] Resumed is whether Started continued a session.」
	Resumed bool
	// 「[Opus 5.5] Type is the update's sessionUpdate, and Raw the update whole, as JSON.」
	Type, Raw string
	// 「[Opus 5.5] Text is a chunk's text, every byte.」
	Text                                      string
	ToolCallID, Title, Name, ToolKind, Status string
	RawInput, RawOutput                       string
	Used, Size                                uint64
	CostAmount, CostCurrency                  string
}

// 「[Opus 5.5] Option is one choice a permission request offers.」
type Option struct{ ID, Name, Kind string }

// 「[Opus 5.5] Asked is a session/request_permission: the tool call it asks about, and the options.」
type Asked struct {
	Session, ToolCallID, Title, ToolKind, RawInput string
	Options                                        []Option
}

// 「[Opus 5.5] Answer is what the agent answered: who it is, the session, its message whole, why it stopped, and the」
// 「[Opus 5.5] context it last reported.」
type Answer struct {
	Agent, AgentVersion string
	Session             string
	Text                string
	Stop                string
	Used, Size          uint64
	Took                time.Duration
}

// 「[Opus 5.5] Error is a JSON-RPC error the agent answered a request with.」
type Error struct {
	Method        string
	Code          int
	Message, Data string
}

func (e *Error) Error() string {
	s := e.Method + " was answered with error " + strconv.Itoa(e.Code) + ": " + e.Message
	if e.Data != "" {
		s += " " + e.Data
	}
	return s
}

// 「[Opus 5.5] rejected is the option a turn no one is asked in answers with: one that rejects, once before always.」
func rejected(a Asked) string {
	for _, kind := range []string{"reject_once", "reject_always"} {
		for _, o := range a.Options {
			if o.Kind == kind {
				return o.ID
			}
		}
	}
	return ""
}

var errNoTurn = errors.New("acp: a turn needs an agent binary, an absolute cwd and something said")
