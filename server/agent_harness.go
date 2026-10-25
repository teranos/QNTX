package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/proto"
)

// A harness the Node Agent runs in (ADR-048). Its sigils, its session and its
// binary have one shape in every harness: one adds what it is called, what its
// sigils say beyond that shape, and its part of a turn.
type harness struct {
	// name is its signum, and where its sigils answer: /api/<name>.
	name string
	// called is the harness as a sentence names it.
	called string
	// file is where the agent's session in it is kept, under the agent's home.
	file string

	// sayTakes is what say takes beyond says, in this harness.
	sayTakes []*protocol.Param

	// named reports whether am.toml names the agent in it, and absent says why
	// it cannot be spoken to there when it does not.
	named  func() bool
	absent func() *protocol.Refusal
	// fetching is what am says while the binary has not arrived.
	fetching string
	// spec is how the Node Agent runs in it, as am.toml names it.
	spec func() agentSpec
	// part is its part of one turn for an agent running as spec says, or why
	// nothing is said to it.
	part func(ctx context.Context, sent sigil.Sent, agent *rootAgent, spec agentSpec) (aTurn, *protocol.Refusal)
	// am is what am answers of an agent in it, running as spec says: what
	// every harness says of the agent in it, and what is the harness's own.
	am func(is agentIn, agent *rootAgent, spec agentSpec) proto.Message
}

// harnessSigil is one sigil a harness has of its own, and what answers it.
type harnessSigil struct {
	sigil  *protocol.Sigil
	answer sigil.Answer
}

// harnesses is every harness the Node Agent runs in.
func (s *QNTXServer) harnesses() []*harness {
	return []*harness{s.claudeHarness(), s.piHarness()}
}

// nodeAgentPath is where the Node Agent's sigils answer, whichever harness a turn runs in.
const nodeAgentPath = "/api/agent"

// "Signa Claude and Pi should not exist"

// "Node Agent is Node agent"

// nodeAgentSignum is the Node Agent's say, am and session, in the harness named, and its sign-in
// to Claude Code.
func (s *QNTXServer) nodeAgentSignum() sigil.Signum {
	harness := &protocol.Param{Name: "harness", Required: true, OneOf: []string{agentLineClaude, agentLinePi}, Says: "The harness it runs in: claude for Claude Code, pi for Pi."}
	claude := s.claudeHarness()
	login := s.loginSigil()
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "agent",
			Description: "The Node Agent: QNTX itself as expressed to ROOT, run by the node in a Coding Agent Harness.",
			Tags:        []string{"agent", "root"},
			Oasf:        []string{"ai_ml_engineering/agent_orchestration/multi_agent_coordination"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "say",
					Does:   "Says something to the Node Agent in the harness named and gives what it answered. In each harness it is one session that continues, written down by the agent as it goes and read with agent session. A picture attached travels by its file id (ADR-054).",
					Takes:  append([]*protocol.Param{harness, {Name: "says", Required: true, Says: "What is said to it."}, screenshotParam()}, claude.sayTakes...),
					Answer: "protocol.NodeAgentSaid",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: nodeAgentPath + "/say"},
				},
				{
					Name:   "am",
					Does:   "Who the Node Agent is and how the node runs it, in the harness named.",
					Takes:  []*protocol.Param{harness},
					Answer: "protocol.NodeAgentAm",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: nodeAgentPath},
				},
				{
					Name:   "session",
					Does:   "The Node Agent's one session in the harness named, whole: everything said to it there by whoever said it, as a transcript.",
					Takes:  []*protocol.Param{harness},
					Answer: "protocol.SessionTranscript",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: nodeAgentPath + "/session"},
				},
				login.sigil,
			},
		},
		Answers: map[string]sigil.Answer{
			"say":     s.nodeAgentSay,
			"am":      s.nodeAgentAm,
			"session": s.nodeAgentSession,
			"login":   login.answer,
		},
	}
}

// harnessNamed is the harness a call names.
func (s *QNTXServer) harnessNamed(sent sigil.Sent) (*harness, *protocol.Refusal) {
	switch sent["harness"] {
	case agentLineClaude:
		return s.claudeHarness(), nil
	case agentLinePi:
		return s.piHarness(), nil
	}
	return nil, notAHarness(sent)
}

func (s *QNTXServer) nodeAgentSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, refused := s.harnessNamed(sent)
	if refused != nil {
		return nil, refused
	}
	answer, refused := s.harnessSay(ctx, h, sent)
	if refused != nil {
		return nil, refused
	}
	switch said := answer.(type) {
	case *protocol.ClaudeSaid:
		return &protocol.NodeAgentSaid{Claude: said}, nil
	case *protocol.PiSaid:
		return &protocol.NodeAgentSaid{Pi: said}, nil
	}
	return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Node Agent answered in " + h.called + " as no Coding Agent Harness this node runs"}
}

func (s *QNTXServer) nodeAgentAm(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, refused := s.harnessNamed(sent)
	if refused != nil {
		return nil, refused
	}
	answer, refused := s.harnessAm(h)
	if refused != nil {
		return nil, refused
	}
	switch am := answer.(type) {
	case *protocol.ClaudeAm:
		return &protocol.NodeAgentAm{Claude: am}, nil
	case *protocol.PiAm:
		return &protocol.NodeAgentAm{Pi: am}, nil
	}
	return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Node Agent said who it is in " + h.called + " as no Coding Agent Harness this node runs"}
}

func (s *QNTXServer) nodeAgentSession(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, refused := s.harnessNamed(sent)
	if refused != nil {
		return nil, refused
	}
	return s.harnessSession(h)
}

// harnessAgent is the agent, when it can be spoken to in h.
func (s *QNTXServer) harnessAgent(h *harness) (*rootAgent, *protocol.Refusal) {
	if s.nodeAgent == nil {
		return nil, s.thereIsNoNodeAgent()
	}
	if !h.named() || s.harnessHeldBy(h.name) == nil {
		return nil, h.absent()
	}
	return s.nodeAgent, nil
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
	screenshot, refused := s.screenshotFor(agent, sent)
	if refused != nil {
		return nil, refused
	}
	t, refused := h.part(ctx, sent, agent, spec)
	if refused != nil {
		return nil, refused
	}
	t.in, t.called, t.element, t.screenshot = agent.in(h), h.called, agent.element, screenshot
	return s.sayInHarness(ctx, caller, agent, sent["says"], t)
}

func (s *QNTXServer) harnessAm(h *harness) (any, *protocol.Refusal) {
	agent, refused := s.harnessAgent(h)
	if refused != nil {
		return nil, refused
	}
	return s.amOf(h, agent, h.spec()), nil
}

// agentIn is what am says of the agent in every harness: its DID, the session
// it continues, whether it is in a turn, where the harness's binary is, and
// why it cannot be spoken to when it cannot.
type agentIn struct {
	did, session, path, notReady string
	answering                    bool
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
