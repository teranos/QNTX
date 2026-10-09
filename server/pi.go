package server

import (
	"context"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/proto"
)

// The ROOT agent in Pi, its other harness (ADR-048).
// "NOT BE ENTIRELY DEPENDENT ON ONE HARNESS"

// piGatewayProvider is what Pi calls the node's gateway among its providers.
const piGatewayProvider = "qntx"

// piHarness is the ROOT agent in Pi.
func (s *QNTXServer) piHarness() *harness {
	named := func() appcfg.PiConfig { return s.deps.cfg.Agent.Root.Pi }
	return &harness{
		name: "pi", called: "Pi", file: piSessionFile,
		description: "The ROOT agent in Pi: the same agent as in Claude Code, in its other harness and a session of its own.",
		sayDoes:     "Says something to the ROOT agent in Pi and gives what it answered. It is its session in Pi, beside the one in Claude Code, read with pi session.",
		amDoes:      "Who the ROOT agent is in Pi and how the node runs it there.",
		sessionDoes: "The ROOT agent's session in Pi, whole: everything said to it there, as a transcript.",
		sayAnswer:   "protocol.PiSaid",
		amAnswer:    "protocol.PiAm",
		named:       func() bool { return named().Named() },
		absent:      s.thereIsNoPi,
		fetching:    "Pi is still being built",
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent) (aTurn, *protocol.Refusal) {
			return s.piPart(named(), agent), nil
		},
		am: func(is agentIn) proto.Message {
			pin := named()
			return &protocol.PiAm{
				Did: is.did, Model: pin.Model, Thinking: pin.Thinking, Gateway: pin.Gateway,
				Session: is.session, Answering: is.answering, Pi: is.path, PiVersion: pi.PinnedVersion, NotReady: is.notReady,
			}
		},
	}
}

// thereIsNoPi is why the ROOT agent cannot be spoken to in Pi on this node.
func (s *QNTXServer) thereIsNoPi() *protocol.Refusal {
	if s.rootAgent == nil {
		return s.thereIsNoRootAgent()
	}
	return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no Pi for the ROOT agent ([agent.root.pi]), so it runs in Claude Code alone"}
}

// piPart is Pi's part of one turn.
func (s *QNTXServer) piPart(named appcfg.PiConfig, agent *rootAgent) aTurn {
	var binary string
	return aTurn{
		effort: named.Thinking,
		ready: func(ctx context.Context, _ string, kept bool) (bool, *protocol.Refusal) {
			var err error
			if binary, err = s.harnessHeldBy("pi").Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Pi to run: " + err.Error()}
			}
			return kept, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			said := s.piSaid(agent, named, binary, t.session, t.says, t.env)
			answer, err := said.Run(s.ctx, func(e pi.Event) { t.write(t.writes.rowsOfPi(e, t.now())) })
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
		System: agent.isSaidToBe(), Env: env,
	}
	if s.ownURL != "" {
		said.MCP = []pi.MCPServer{{Name: rootAgentMCP, URL: s.ownURL + "/mcp", Bearer: agent.token}}
		said.Provider = &pi.Provider{
			Name: piGatewayProvider, BaseURL: s.ownURL + "/api/" + named.Gateway + "/v1",
			Key: agent.token, Models: []string{named.Model},
		}
	}
	return said
}
