package server

import (
	"context"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// The ROOT agent in Pi, its other harness (ADR-048).
// "NOT BE ENTIRELY DEPENDENT ON ONE HARNESS"

// piGatewayProvider is what Pi calls the node's gateway among its providers.
const piGatewayProvider = "qntx"

func (s *QNTXServer) piSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "pi",
			Description: "The ROOT agent in Pi: the same agent as in Claude Code, in its other harness and a session of its own.",
			Tags:        []string{"agent", "pi", "root"},
			Sigils: []*protocol.Sigil{
				{
					Name: "say",
					Does: "Says something to the ROOT agent in Pi and gives what it answered. It is its session in Pi, beside the one in Claude Code, read with pi session.",
					Takes: []*protocol.Param{
						{Name: "says", Required: true, Says: "What is said to it."},
					},
					Gives: []*protocol.Field{
						{Name: "answer", Says: "What it answered."},
						{Name: "is_error", Says: "Whether the turn ended in error, in the answer's words."},
						{Name: "stop", Says: "Why Pi says the turn stopped."},
						{Name: "session", Says: "The session it was said in."},
						{Name: "model", Says: "The model that answered."},
						{Name: "pi_version", Says: "The Pi that ran."},
						{Name: "cost_usd", Says: "What Pi says the turn's model calls cost."},
						{Name: "took_ms", Says: "How long the turn took."},
						{Name: "unwritten", Says: "Why a row of the session was not written down, when one was not."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/pi/say"},
				},
				{
					Name: "am",
					Does: "Who the ROOT agent is in Pi and how the node runs it there.",
					Gives: []*protocol.Field{
						{Name: "did", Says: "Its own DID, which signs what it writes down."},
						{Name: "model", Says: "The model am.toml names for Pi."},
						{Name: "thinking", Says: "The thinking level am.toml names for Pi."},
						{Name: "gateway", Says: "The plugin every model call Pi makes goes through."},
						{Name: "session", Says: "The session it continues in Pi, or empty before anything was said to it there."},
						{Name: "answering", Says: "Whether it is in a turn in Pi now."},
						{Name: "pi", Says: "Where the Pi it runs is, or empty when the node has none yet."},
						{Name: "pi_version", Says: "The Pi this build pins."},
						{Name: "not_ready", Says: "Why it cannot be spoken to in Pi, when it cannot."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/pi"},
				},
				{
					Name: "session",
					Does: "The ROOT agent's session in Pi, whole: everything said to it there, as a transcript.",
					Gives: []*protocol.Field{
						{Name: "transcript", Says: "Its session in Pi as turns, each naming the attestation it was read from. Empty before anything was said to it there.", Message: "protocol.Transcript"},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/pi/session"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"say": s.piSay, "am": s.piAm, "session": s.piSession},
	}
}

// thereIsNoPi is why the ROOT agent cannot be spoken to in Pi on this node.
func (s *QNTXServer) thereIsNoPi() *protocol.Refusal {
	if s.rootAgent == nil {
		return s.thereIsNoRootAgent()
	}
	return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no Pi for the ROOT agent ([agent.root.pi]), so it runs in Claude Code alone"}
}

func (s *QNTXServer) piSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what is said to the ROOT agent is written down with who said it, and this asking carried no request"}
	}
	agent := s.rootAgent
	named := s.deps.cfg.Agent.Root.Pi
	if agent == nil || !named.Named() || s.pi == nil {
		return nil, s.thereIsNoPi()
	}
	if spokenBy(caller) == agent.did {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: "the ROOT agent does not speak to itself: it is in the turn that asked"}
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no system to write the ROOT agent's session in: " + err.Error()}
	}

	select {
	case agent.piTurn <- struct{}{}:
		defer func() { <-agent.piTurn }()
	case <-ctx.Done():
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the ROOT agent was still answering somebody else when this caller left"}
	}

	binary, err := s.pi.Path(ctx)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Pi to run: " + err.Error()}
	}
	session, kept, err := agent.sessionIn(piSessionFile)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}

	agent.piAnswering.Store(&turnInSession{session: session})
	defer agent.piAnswering.Store(nil)

	writes := sessionWriter{did: agent.did, session: session, resumed: kept, effort: named.Thinking}
	unwritten := ""
	write := func(rows []*types.As, err error) {
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
			s.logger.Errorw("a row of the ROOT agent's session was not written", "session", session, "harness", "pi", "error", err)
			if unwritten == "" {
				unwritten = err.Error()
			}
		}
	}
	itsGit, err := s.gitEnvironment(agent)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the ROOT agent's git was not set up, so nothing was said to it: " + err.Error()}
	}
	told, err := writes.told(sent["says"], spokenBy(caller), time.Now())
	write([]*types.As{told}, err)

	said := s.piSaid(agent, named, binary, session, sent["says"], itsGit)
	answer, err := said.Run(s.ctx, func(e pi.Event) { write(writes.rowsOfPi(e, time.Now())) })
	if err != nil {
		write(writes.rowsOfPi(pi.Event{Type: "message_end", Message: &pi.Message{Role: "assistant", StopReason: "error", ErrorMessage: err.Error()}}, time.Now()))
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "Pi did not answer: " + err.Error()}
	}
	if !kept {
		if err := agent.keepIn(piSessionFile, session); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "it answered, and the session it answered in was not kept: " + err.Error()}
		}
	}
	return map[string]any{
		"answer": answer.Text, "is_error": answer.IsError, "stop": answer.Stop,
		"session": session, "model": answer.Model, "pi_version": pi.PinnedVersion,
		"cost_usd": answer.CostUSD, "took_ms": answer.Took.Milliseconds(), "unwritten": unwritten,
	}, nil
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

func (s *QNTXServer) piAm(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	agent := s.rootAgent
	named := s.deps.cfg.Agent.Root.Pi
	if agent == nil || !named.Named() || s.pi == nil {
		return nil, s.thereIsNoPi()
	}
	is := map[string]any{
		"did": agent.did, "model": named.Model, "thinking": named.Thinking, "gateway": named.Gateway,
		"session": "", "answering": false, "pi": "", "pi_version": pi.PinnedVersion, "not_ready": "",
	}
	session, kept, err := agent.sessionIn(piSessionFile)
	if err != nil {
		is["not_ready"] = err.Error()
	} else if kept {
		is["session"] = session
	}
	if going := agent.piAnswering.Load(); going != nil {
		is["answering"], is["session"] = true, going.session
	}
	switch path, arrived, err := s.pi.Now(); {
	case !arrived:
		is["not_ready"] = "Pi is still being built"
	case err != nil:
		is["not_ready"] = err.Error()
	default:
		is["pi"] = path
	}
	return is, nil
}

// piSession reads the agent's session in Pi from where it is written.
func (s *QNTXServer) piSession(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	agent := s.rootAgent
	if agent == nil || !s.deps.cfg.Agent.Root.Pi.Named() {
		return nil, s.thereIsNoPi()
	}
	return s.readAgentSession(agent, piSessionFile, agent.piAnswering.Load())
}
