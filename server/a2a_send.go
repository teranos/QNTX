package server

import (
	"context"
	"net/http"
	"slices"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// SendMessage at a tenant (§3.1.1): what is sent to a namespace agent is said
// to it, as agents say says it, and its answer is what it said back. "An
// agent's work outlives the request that brought it" (ADR-048): the turn runs
// whether or not whoever sent it waits.

// a2aSkill is the sigil a message sent to a namespace agent reaches: the one
// skill its card names.
const a2aSkill = "say"

// a2aAgentAt is the agent served at namespace for the caller of r, when the
// a2a: lines let the caller reach its skill (ADR-039: "a2a:<signum> lines say
// who reaches a skill").
func (s *QNTXServer) a2aAgentAt(r *http.Request, namespace string) (anAgentHeld, error) {
	agents := s.agentsSignum()
	at := slices.IndexFunc(agents.GetSigils(), func(held *protocol.Sigil) bool { return held.GetName() == a2aSkill })
	if at < 0 {
		return anAgentHeld{}, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the agents signum has no " + a2aSkill + " for a message to reach"}
	}
	admitted, known := auth.AdmissionFrom(r.Context())
	reaching, anyone := s.reachingOver(reach.OverA2A, heldBy{signum: agents.GetName(), sigil: agents.GetSigils()[at]})
	if !offeredTo(admitted, known, reaching, anyone) {
		return anAgentHeld{}, a2a.NotFound("no skill " + a2aSkill + " of the agent of " + namespace + " that you reach over A2A")
	}
	// A sigil's refusal is a pointer, nil when it answered.
	held, refused := s.namespaceAgent(sigil.WithCaller(r.Context(), r), sigil.Sent{"namespace": namespace})
	if refused != nil {
		return anAgentHeld{}, a2aRefusal(refused)
	}
	return held, nil
}

// a2aRefusal is a sigil's refusal as A2A says one: what does not exist is not
// found (§3.3.2), what was asked wrongly is invalid, and the rest is the
// node's failing.
func a2aRefusal(refused *protocol.Refusal) *a2a.Error {
	switch refused.GetWhy() {
	case sigil.NotFound:
		return a2a.NotFound(refusalSays(refused))
	case sigil.Missing, sigil.Invalid, sigil.NotAllowed:
		return a2a.Invalid(refusalSays(refused))
	}
	return &a2a.Error{Message: refusalSays(refused)}
}

// a2aSession is the session the agent continues: a2a's contextId, since a
// namespace agent is "one session that continues" (agents say). False is an
// agent spoken to never yet.
func (s *QNTXServer) a2aSession(held anAgentHeld) (string, bool, error) {
	return held.agent.sessionIn(held.agent.in(s.claudeHarness()).file)
}

// a2aContext refuses a contextId the agent does not continue: "If an agent
// cannot accept a client-provided contextId, it MUST reject the request with
// an error and MUST NOT generate a new contextId" (§3.4.1).
func (s *QNTXServer) a2aContext(held anAgentHeld, send a2a.Send) error {
	if !send.NamesContext {
		return nil
	}
	session, kept, err := s.a2aSession(held)
	if err != nil {
		return err
	}
	if !kept {
		return a2a.Invalid("contextId " + send.ContextID + " is no session of " + held.agent.called + ": it has not been spoken to yet, and its first turn begins the one session it continues")
	}
	if send.ContextID != session {
		return a2a.Invalid("contextId " + send.ContextID + " is not the session " + held.agent.called + " continues, " + session + ": it has one")
	}
	return nil
}

// turnBegun is what was said, written down as the turn's first row, or why it
// was not.
type turnBegun struct {
	told *types.As
	err  error
}

// turnEnded is how the turn ended: what the agent answered, or why it did not.
type turnEnded struct {
	said *protocol.ClaudeSaid
	err  error
}

// a2aSay says text to the agent for the caller of r, on a turn of its own: the
// caller's leaving does not end it. begun is told when the turn begins and
// ended when it ends; each is told once.
func (s *QNTXServer) a2aSay(r *http.Request, held anAgentHeld, text string) (<-chan turnBegun, <-chan turnEnded) {
	begun, ended := make(chan turnBegun, 1), make(chan turnEnded, 1)
	caller := r.WithContext(context.WithoutCancel(r.Context()))
	sent := sigil.Sent{"namespace": held.agent.namespace, "says": text}
	go func() {
		answer, refused := s.sayHeard(sigil.WithCaller(caller.Context(), caller), s.claudeHarness(), held.agent, held.line.spec, sent,
			func(told *types.As, unwritten error) { begun <- turnBegun{told: told, err: unwritten} })
		if refused != nil {
			ended <- turnEnded{err: a2aRefusal(refused)}
			return
		}
		said, isClaude := answer.(*protocol.ClaudeSaid)
		if !isClaude {
			ended <- turnEnded{err: &a2a.Error{Name: "InvalidAgentResponseError", Message: held.agent.called + " did not answer as Claude Code"}}
			return
		}
		ended <- turnEnded{said: said}
	}()
	return begun, ended
}
