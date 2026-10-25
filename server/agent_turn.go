package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// A turn is the same in every harness the Node Agent runs in (ADR-048): who
// said it, one at a time, written down as the agent, and the session kept.
// What differs is the harness's part, an aTurn.

// aTurn is a harness's part of one turn.
type aTurn struct {
	// Codex owns its thread ID and process environment; it needs no Claude binary.
	withoutClaude bool
	// Keep the QNTX transcript session even when the first provider turn fails.
	keepBeforeRun bool
	in            *inHarness
	// called is the harness as a refusal names it.
	called string
	// effort is what the harness runs the turn at, as a transcript reads it.
	effort string
	// element is the element the turn is said in, and screenshot where its
	// screenshot was put for the agent, each empty when none (ADR-054).
	element, screenshot string
	// ready is what the harness needs before anything is said to it, and
	// whether it resumes session. A refusal here says nothing to it.
	ready func(ctx context.Context, session string, kept bool) (resumes bool, refused *protocol.Refusal)
	// run says it in the harness and answers, under the node's own context and
	// not the caller's: a caller that leaves does not stop what it asked for
	// halfway. It writes each row its stream says happened, and a row of its
	// failing when it does not answer.
	run func(t turnRun) (proto.Message, error)
}

// turnRun is what a harness runs a turn with.
type turnRun struct {
	says, session string
	resumes       bool
	// env is the agent's git, as the harness hands it on.
	env    []string
	writes sessionWriter
	write  func([]*types.As, error)
	// unwritten is why a row of the session was not written down, when one
	// was not, as of the rows written so far.
	unwritten func() string
	// started is told the stream the turn prints to once it runs.
	started func(stream string)
}

func (turnRun) now() time.Time { return time.Now() }

// sayInHarness is one turn of agent in the harness t is the part of.
func (s *QNTXServer) sayInHarness(ctx context.Context, caller *http.Request, agent *rootAgent, says string, t aTurn) (any, *protocol.Refusal) {
	// Its token is ROOT's kind, and it would be asking from inside the turn
	// it then waits on.
	if spokenBy(caller) == agent.did {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: agent.called + " does not speak to itself: it is in the turn that asked"}
	}
	store, err := s.sessionStoreOf(caller, agent)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "nowhere to write the session of " + agent.called + ": " + err.Error()}
	}

	select {
	case t.in.turn <- struct{}{}:
		defer func() { <-t.in.turn }()
	case <-ctx.Done():
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " was still answering somebody else when this caller left"}
	}

	session, kept, err := agent.sessionIn(t.in.file)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	resumes, refused := t.ready(ctx, session, kept)
	if refused != nil {
		return nil, refused
	}

	t.in.answering.Store(&turnInSession{session: session})
	defer t.in.answering.Store(nil)

	writes := sessionWriter{did: agent.did, session: session, resumed: resumes, effort: t.effort, element: t.element, screenshot: t.screenshot}
	write, unwritten := s.sessionRows(agent, store, session, t.called)
	// The Node Agent's git is its own (ADR-048, Its git). A namespace agent has
	// none: its ground is handed the agent's own token, and writes in its namespace.
	itsGit := []string{"QNTX_TOKEN=" + agent.token}
	if agent.namespace == auth.NamespaceSystem {
		if itsGit, err = s.gitEnvironment(agent); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Node Agent's git was not set up, so nothing was said to it: " + err.Error()}
		}
	}
	// Its ground is the one its hooks run (ADR-050, Hosted agents under ground),
	// and the node's Claude Code the one a ritual it performs starts its agent with (ground ritual/run.d).
	if !t.withoutClaude {
		claude, err := s.harnessHeldBy("claude").Path(ctx)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no Claude Code for a ritual " + agent.called + " performs to start: " + err.Error()}
		}
		itsGit = append(itsGit, "PATH="+comet.AgentPath(agent.home, filepath.Dir(claude)+":"+os.Getenv("PATH")))
	}

	if t.keepBeforeRun && !kept {
		if err := agent.keepIn(t.in.file, session); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
	}

	told, err := writes.told(says, spokenBy(caller), time.Now())
	write([]*types.As{told}, err)

	// A turn asked for a Task is told to it as it starts (A2A §3.1.1); a say
	// that is no Task's tells nobody.
	started := func(string) {}
	if starts, forATask := taskStartsIn(ctx); forATask {
		started = func(stream string) { starts(session, stream) }
	}
	answer, err := t.run(turnRun{says: shakePrompt(says, t.element, t.screenshot), session: session, resumes: resumes, env: itsGit, writes: writes, write: write,
		unwritten: unwritten, started: started})

	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: t.called + " did not answer: " + err.Error()}
	}
	if !kept {
		if err := agent.keepIn(t.in.file, session); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "it answered, and the session it answered in was not kept: " + err.Error()}
		}
	}
	return answer, nil
}

// sessionRows writes rows of agent's session down in store. The session is the
// agent's to write down, signed as itself. A row that does not land is said by
// unwritten, as of the rows written so far, and never stops the turn.
func (s *QNTXServer) sessionRows(agent *rootAgent, store ats.AttestationStore, session, harness string) (write func([]*types.As, error), unwritten func() string) {
	why := ""
	write = func(rows []*types.As, err error) {
		if err == nil {
			for _, row := range rows {
				if err = agent.signer.Sign(row); err != nil {
					break
				}
				if err = store.CreateAttestation(row); err != nil {
					break
				}
			}
		}
		if err != nil {
			s.logger.Errorw("a row of the agent's session was not written", "agent", agent.did, "session", session, "harness", harness, "error", err)
			if why == "" {
				why = err.Error()
			}
		}
	}
	return write, func() string { return why }
}

// sessionStoreOf is where an agent's session is written: system for ROOT's,
// whose sessions are the node's own record, and its namespace for a namespace
// agent, which stands there and nothing crosses (ADR-026). The namespace is
// written as the caller is admitted to act there.
func (s *QNTXServer) sessionStoreOf(caller *http.Request, agent *rootAgent) (ats.AttestationStore, error) {
	if agent.namespace == auth.NamespaceSystem {
		return s.held.WriteWhatTheNodeKnowsOfItself()
	}
	admitted, gated := auth.AdmissionFrom(caller.Context())
	if !gated {
		return nil, errors.Newf("the session of %s is written in %s as its caller acts there, and this caller was admitted nowhere", agent.called, agent.namespace)
	}
	return s.held.Write(admitted, agent.namespace)
}
