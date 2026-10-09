package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// A harness the ROOT agent runs in (ADR-048). Its sigils, its session and its
// binary have one shape in every harness: one adds what it is called, what its
// sigils say beyond that shape, and its part of a turn.
type harness struct {
	// name is its signum, and where its sigils answer: /api/<name>.
	name string
	// called is the harness as a sentence names it.
	called string
	// file is where the agent's session in it is kept, under the agent's home.
	file string

	description, sayDoes, amDoes, sessionDoes string
	// sayTakes is what say takes beyond says.
	sayTakes          []*protocol.Param
	sayGives, amGives []*protocol.Field

	// named reports whether am.toml names the agent in it, and absent says why
	// it cannot be spoken to there when it does not.
	named  func() bool
	absent func() *protocol.Refusal
	// pathKey is what am calls where its binary is, and fetching what am says
	// while the binary has not arrived.
	pathKey, fetching string
	// spec is how the ROOT agent runs in it, as am.toml names it.
	spec func() agentSpec
	// part is its part of one turn for an agent running as spec says, or why
	// nothing is said to it.
	part func(ctx context.Context, sent sigil.Sent, agent *rootAgent, spec agentSpec) (aTurn, *protocol.Refusal)
	// am says what is the harness's own of an agent in it, running as spec says.
	am func(is map[string]any, agent *rootAgent, spec agentSpec)
	// also is what this harness has beyond say, am and session.
	also []harnessSigil
}

// harnessSigil is one sigil a harness has of its own, and what answers it.
type harnessSigil struct {
	sigil  *protocol.Sigil
	answer sigil.Answer
}

// harnesses is every harness the ROOT agent runs in.
func (s *QNTXServer) harnesses() []*harness {
	return []*harness{s.claudeHarness(), s.piHarness()}
}

// harnessSigna is each harness's sigils.
func (s *QNTXServer) harnessSigna() []sigil.Signum {
	var signa []sigil.Signum
	for _, h := range s.harnesses() {
		signa = append(signa, s.harnessSignum(h))
	}
	return signa
}

// harnessSignum is a harness's say, am and session.
func (s *QNTXServer) harnessSignum(h *harness) sigil.Signum {
	at := "/api/" + h.name
	answers := map[string]sigil.Answer{
		"say":     func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) { return s.harnessSay(ctx, h, sent) },
		"am":      func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) { return s.harnessAm(h) },
		"session": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) { return s.harnessSession(h) },
	}
	var also []*protocol.Sigil
	for _, own := range h.also {
		also = append(also, own.sigil)
		answers[own.sigil.GetName()] = own.answer
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        h.name,
			Description: h.description,
			Tags:        []string{"agent", h.name, "root"},
			Sigils: append([]*protocol.Sigil{
				{
					Name:  "say",
					Does:  h.sayDoes,
					Takes: append([]*protocol.Param{{Name: "says", Required: true, Says: "What is said to it."}}, h.sayTakes...),
					Gives: h.sayGives,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: at + "/say"},
				},
				{
					Name:  "am",
					Does:  h.amDoes,
					Gives: h.amGives,
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				{
					Name:   "session",
					Does:   h.sessionDoes,
					Answer: "protocol.SessionTranscript",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: at + "/session"},
				},
			}, also...),
		},
		Answers: answers,
	}
}

// harnessAgent is the agent, when it can be spoken to in h.
func (s *QNTXServer) harnessAgent(h *harness) (*rootAgent, *protocol.Refusal) {
	if s.rootAgent == nil {
		return nil, s.thereIsNoRootAgent()
	}
	if !h.named() || s.harnessHeldBy(h.name) == nil {
		return nil, h.absent()
	}
	return s.rootAgent, nil
}

func (s *QNTXServer) harnessSay(ctx context.Context, h *harness, sent sigil.Sent) (any, *protocol.Refusal) {
	agent, refused := s.harnessAgent(h)
	if refused != nil {
		return nil, refused
	}
	return s.sayTo(ctx, h, agent, h.spec(), sent)
}

// sayTo says something to one agent in h, running as spec says.
func (s *QNTXServer) sayTo(ctx context.Context, h *harness, agent *rootAgent, spec agentSpec, sent sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what is said to " + agent.called + " is written down with who said it, and this asking carried no request"}
	}
	t, refused := h.part(ctx, sent, agent, spec)
	if refused != nil {
		return nil, refused
	}
	t.in, t.called = agent.in(h), h.called
	return s.sayInHarness(ctx, caller, agent, sent["says"], t)
}

func (s *QNTXServer) harnessAm(h *harness) (any, *protocol.Refusal) {
	agent, refused := s.harnessAgent(h)
	if refused != nil {
		return nil, refused
	}
	return s.amOf(h, agent, h.spec()), nil
}

// amOf is who one agent in h is and how the node runs it.
func (s *QNTXServer) amOf(h *harness, agent *rootAgent, spec agentSpec) map[string]any {
	in := agent.in(h)
	is := map[string]any{"did": agent.did, "session": "", "answering": false, h.pathKey: "", "not_ready": ""}
	h.am(is, agent, spec)
	session, kept, err := agent.sessionIn(in.file)
	if err != nil {
		is["not_ready"] = err.Error()
	} else if kept {
		is["session"] = session
	}
	// A first turn is in a session not kept yet, and is read all the same.
	if going := in.answering.Load(); going != nil {
		is["answering"], is["session"] = true, going.session
	}
	// Asked without waiting: a fetch still going is said, not sat through.
	switch path, arrived, err := s.harnessHeldBy(h.name).Now(); {
	case !arrived:
		is["not_ready"] = h.fetching
	case err != nil:
		is["not_ready"] = err.Error()
	default:
		is[h.pathKey] = path
	}
	return is
}

// harnessSession reads the agent's session in h from where it is written, so
// whoever may talk to it reads all of it, wherever they stand.
func (s *QNTXServer) harnessSession(h *harness) (any, *protocol.Refusal) {
	agent, refused := s.harnessAgent(h)
	if refused != nil {
		return nil, refused
	}
	return s.readAgentSession(agent, agent.in(h))
}

// harnessesHeld is each harness's binary on this node, by the harness's name.
type harnessesHeld struct {
	mu   sync.Mutex
	held map[string]*harnessHeld
}

// harnessHeldBy is the binary of the harness named, or nil when the node holds none.
func (s *QNTXServer) harnessHeldBy(name string) *harnessHeld {
	s.harnessBinaries.mu.Lock()
	defer s.harnessBinaries.mu.Unlock()
	return s.harnessBinaries.held[name]
}

// holdHarness keeps the binary of the harness named.
func (s *QNTXServer) holdHarness(name string, held *harnessHeld) {
	s.harnessBinaries.mu.Lock()
	defer s.harnessBinaries.mu.Unlock()
	if s.harnessBinaries.held == nil {
		s.harnessBinaries.held = map[string]*harnessHeld{}
	}
	s.harnessBinaries.held[name] = held
}
