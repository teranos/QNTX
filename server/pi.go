package server

import (
	"context"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/proto"
)

// The Node Agent in Pi, its other harness (ADR-048).
// "NOT BE ENTIRELY DEPENDENT ON ONE HARNESS"

// piGatewayProvider is what Pi calls the node's gateway among its providers.
const piGatewayProvider = "qntx"

// piHarness is the Node Agent in Pi.
func (s *QNTXServer) piHarness() *harness {
	named := func() appcfg.PiConfig { return s.deps.cfg.Agent.Root.Pi }
	return &harness{
		name: "pi", called: "Pi", file: piSessionFile,
		description: "The Node Agent in Pi: the same agent as in Claude Code, in its other harness and a session of its own.",
		sayDoes:     "Says something to the Node Agent in Pi and gives what it answered. It is its session in Pi, beside the one in Claude Code, read with pi session.",
		amDoes:      "Who the Node Agent is in Pi and how the node runs it there.",
		sessionDoes: "The Node Agent's session in Pi, whole: everything said to it there, as a transcript.",
		sayAnswer:   "protocol.PiSaid",
		amAnswer:    "protocol.PiAm",
		named:       func() bool { return named().Named() },
		absent:      s.thereIsNoPi,
		fetching:    "Pi is still being built",
		spec:        func() agentSpec { return agentSpec{} },
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
			return s.piPart(named(), agent), nil
		},
		am: func(is agentIn, _ *rootAgent, _ agentSpec) proto.Message {
			pin := named()
			return &protocol.PiAm{
				Did: is.did, Model: pin.Model, Thinking: pin.Thinking, Gateway: pin.Gateway,
				Session: is.session, Answering: is.answering, Pi: is.path, PiVersion: pi.PinnedVersion, NotReady: is.notReady,
			}
		},
	}
}

// thereIsNoPi is why the Node Agent cannot be spoken to in Pi on this node.
func (s *QNTXServer) thereIsNoPi() *protocol.Refusal {
	if s.nodeAgent == nil {
		return s.thereIsNoNodeAgent()
	}
	return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no Pi for the Node Agent ([agent.root.pi]), so it runs in Claude Code alone"}
}

// piPart is Pi's part of one turn of the Node Agent.
func (s *QNTXServer) piPart(named appcfg.PiConfig, agent *rootAgent) aTurn {
	return s.piTurn(named.Thinking, func(binary, session, says string, env []string) pi.Said {
		return s.piSaid(agent, named, binary, session, says, env)
	})
}

// piTurn is Pi's part of one turn, run as said gives it.
func (s *QNTXServer) piTurn(thinking string, said func(binary, session, says string, env []string) pi.Said) aTurn {
	var binary string
	return aTurn{
		effort: thinking,
		ready: func(ctx context.Context, _ string, kept bool) (bool, *protocol.Refusal) {
			var err error
			if binary, err = s.harnessHeldBy("pi").Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Pi to run: " + err.Error()}
			}
			return kept, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			answer, err := said(binary, t.session, t.says, t.env).Run(s.ctx, func(e pi.Event) { t.write(t.writes.rowsOfPi(e, t.now())) })
			if err != nil {
				t.write(t.writes.rowsOfPi(pi.Event{Type: "message_end", Message: &pi.Message{Role: "assistant", StopReason: "error", ErrorMessage: err.Error()}}, t.now()))
				return nil, err
			}
			return &protocol.PiSaid{
				Answer: answer.Text, IsError: answer.IsError, Stop: answer.Stop,
				Session: t.session, Model: answer.Model, PiVersion: pi.PinnedVersion,
				CostUsd: answer.CostUSD, TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
			}, nil
		},
	}
}

// piSaid is how one turn is run in Pi: the node's MCP and its gateway, each
// reached with the agent's own token.
func (s *QNTXServer) piSaid(agent *rootAgent, named appcfg.PiConfig, binary, session, says string, env []string) pi.Said {
	said := pi.Said{
		Binary: binary, Home: agent.home, Session: session, Says: says,
		Model: piGatewayProvider + "/" + named.Model, Thinking: named.Thinking,
		System: agent.isSaidToBe(), Env: env, Tools: pi.PisOwnTools,
	}
	if s.ownURL != "" {
		said.MCP = s.piMCP(agent, s.ownURL)
		said.Provider = &pi.Provider{
			Name: piGatewayProvider, BaseURL: s.ownURL + "/api/" + named.Gateway + "/v1",
			Key: agent.token, Models: []string{named.Model},
		}
	}
	return said
}

// piMCP is the node's MCP as an agent in Pi reaches it, at own, with the
// agent's own token.
func (s *QNTXServer) piMCP(agent *rootAgent, own string) []pi.MCPServer {
	return []pi.MCPServer{{Name: nodeAgentMCP, URL: own + "/mcp", Bearer: agent.token}}
}

// namespacePiPart is Pi's part of one turn of a namespace's agent: its model
// calls made at its provider on the namespace's key of that name, handed to
// the process and offered to no model (ADR-051).
func (s *QNTXServer) namespacePiPart(held aPiAgentHeld) (aTurn, *protocol.Refusal) {
	agent, line := held.agent, held.line
	variable, known := piProviders[line.provider]
	if !known {
		return aTurn{}, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + agent.namespace + " in Pi names provider " + line.provider + ", which is none Pi is run at"}
	}
	own, err := named.TextOf("this node's own URL, where the agent of "+agent.namespace+" reaches its MCP,", s.ownURL)
	if err != nil {
		return aTurn{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	key, err := s.keyValue(agent.namespace, line.provider)
	if err != nil {
		return aTurn{}, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + agent.namespace + " in Pi makes its model calls on the namespace's " + line.provider + " key: " + err.Error()}
	}
	return s.piTurn(line.thinking, func(binary, session, says string, env []string) pi.Said {
		return pi.Said{
			Binary: binary, Home: agent.home, Session: session, Says: says,
			Model: line.provider + "/" + line.model, Thinking: line.thinking,
			System: agent.isSaidToBe(), MCP: s.piMCP(agent, own.String()),
			Env: append(env, variable+"="+key),
			// Pi asks before no tool call and has no sandbox (ADR-048), so a
			// namespace's agent runs none of Pi's own tools: the node's MCP
			// alone.
			Tools: pi.NoneOfPisOwn,
		}
	}), nil
}
