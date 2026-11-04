// Package acp runs an Agent Client Protocol agent as a harness, through libACP's client side.
//
// "can't we build the client into libACP and make QNTX use it as an optional subsystem? Just like how I
// would like any harness to be pluggable"
//
// A harness that speaks ACP is a binary and its arguments: Run starts it, signs in, starts or continues
// its session, sets the session's options, says one thing, and hands each update the agent sends to the
// caller. Every message is written and read by libACP (github.com/teranos/ground, libacp/), linked with
// the build tag libacp; without it Run refuses.
package acp

import (
	"strconv"
	"time"

	errors "github.com/teranos/sacred-error"
)

// Agent is the ACP agent's process: its binary, arguments, directory and environment, whole.
type Agent struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string
}

// Turn is one thing said to the agent in a session.
type Turn struct {
	// Cwd is the session's working directory, absolute.
	Cwd string
	// Session is the agent's session to continue; empty starts one.
	Session string
	// Auth is the authMethods id signed in with before any session; empty signs in with none.
	Auth string
	// Config is each session config option and the value id it is set to, in order, before the prompt.
	Config [][2]string
	Says   string
	// Permission chooses the option a session/request_permission is answered with, "" cancelling the
	// turn. Nil answers with a reject option the agent offered, and cancels when it offered none.
	Permission func(Asked) string
	// Client and Version are who the client says it is.
	Client, Version string
}

// UpdateKind is what an update is, as the client reads it.
type UpdateKind int

const (
	// Started is the session the turn runs in, started or continued, before anything else.
	Started UpdateKind = iota
	Other
	UserMessage
	AgentMessage
	AgentThought
	ToolCall
	ToolCallUpdate
	Usage
)

// Update is one session/update, or the Started that leads them. A field the agent did not send is empty.
type Update struct {
	Kind    UpdateKind
	Session string
	// Resumed is whether Started continued a session.
	Resumed bool
	// Type is the update's sessionUpdate, and Raw the update whole, as JSON.
	Type, Raw string
	// Text is a chunk's text, every byte.
	Text                                      string
	ToolCallID, Title, Name, ToolKind, Status string
	RawInput, RawOutput                       string
	Used, Size                                uint64
	CostAmount, CostCurrency                  string
}

// Option is one choice a permission request offers.
type Option struct{ ID, Name, Kind string }

// Asked is a session/request_permission: the tool call it asks about, and the options.
type Asked struct {
	Session, ToolCallID, Title, ToolKind, RawInput string
	Options                                        []Option
}

// Answer is what the agent answered: who it is, the session, its message whole, why it stopped, and the
// context it last reported.
type Answer struct {
	Agent, AgentVersion string
	Session             string
	Text                string
	Stop                string
	Used, Size          uint64
	Took                time.Duration
}

// Error is a JSON-RPC error the agent answered a request with.
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

// rejected is the option a turn no one is asked in answers with: one that rejects, once before always.
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
