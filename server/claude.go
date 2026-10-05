package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/claudecode"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// The ROOT agent, as the node runs it. What it is and why is ADR-048's.

// rootAgentPurpose is what the ROOT agent's key is derived from the node's for.
const rootAgentPurpose = "agent:root"

// rootAgentMCP is the name the node's own MCP has to the agent.
const rootAgentMCP = "qntx"

// rootAgent is the ROOT agent this node runs: who it is, where it works, and
// the one session it continues.
type rootAgent struct {
	did    string
	signer *signing.Signer
	// token is its own, and what it presents at the node's MCP.
	token string
	home  string
	// turn holds one value while it is being spoken to: it is one session, and
	// what is said to it is heard one at a time.
	turn chan struct{}
}

// theRootAgent is the ROOT agent of the node holding this key, working in home.
func theRootAgent(node ed25519.PrivateKey, home string) (*rootAgent, error) {
	key, err := access.DeriveKey(node, rootAgentPurpose)
	if err != nil {
		return nil, err
	}
	token, did, err := access.DeriveToken(node, rootAgentPurpose)
	if err != nil {
		return nil, err
	}
	return &rootAgent{did: did, signer: signing.NewSigner(key, did), token: token, home: home, turn: make(chan struct{}, 1)}, nil
}

// rootAgentHome is where a node keeps its ROOT agent: its Claude Code
// configuration, its work, and the session it continues.
func rootAgentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve the home directory the ROOT agent is kept under")
	}
	return filepath.Join(home, ".qntx", "agents", "root"), nil
}

// session is the one session it continues, and whether Claude Code holds it
// already. Before anything was said to it, it is a new id.
func (a *rootAgent) session() (id string, resumes bool, err error) {
	kept, err := os.ReadFile(filepath.Join(a.home, "session"))
	if os.IsNotExist(err) {
		return uuid.NewString(), false, nil
	}
	if err != nil {
		return "", false, errors.Wrap(err, "the ROOT agent's session did not read")
	}
	return strings.TrimSpace(string(kept)), true, nil
}

// keep writes down the session it continues from here on.
func (a *rootAgent) keep(id string) error {
	if err := os.MkdirAll(a.home, 0o700); err != nil {
		return errors.Wrapf(err, "could not create %s", a.home)
	}
	path := filepath.Join(a.home, "session")
	return errors.Wrapf(os.WriteFile(path, []byte(id+"\n"), 0o600), "could not write %s", path)
}

// isSaidToBe is what the ROOT agent additionally is, said to Claude Code with
// everything said to it.
func (a *rootAgent) isSaidToBe() string {
	return "You are the ROOT agent of a QNTX node: the node itself, as ROOT speaks to it. " +
		"You run on the machine the node runs on, as the user the node runs as. " +
		"Your DID is " + a.did + ". " +
		"The node's sigils are the tools of the MCP server named " + rootAgentMCP + ", which you reach with your own token."
}

func (s *QNTXServer) claudeSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "claude",
			Description: "The ROOT agent: Claude Code, run by the node as itself.",
			Sigils: []*protocol.Sigil{
				{
					Name: "say",
					Does: "Says something to the ROOT agent and gives what it answered. It is one session that continues, written down by the agent as it goes and read with transcripts read.",
					Takes: []*protocol.Param{
						{Name: "says", Required: true, Says: "What is said to it."},
						{Name: "permission_mode", OneOf: appcfg.PermissionModes, Says: "The permission mode Claude Code runs this in. Not sent, it is the one am.toml gives."},
					},
					Gives: []*protocol.Field{
						{Name: "answer", Says: "What it answered."},
						{Name: "is_error", Says: "Whether Claude Code reports the turn as failed, in the answer's words."},
						{Name: "subtype", Says: "How Claude Code says the turn ended."},
						{Name: "session", Says: "The session it was said in."},
						{Name: "model", Says: "The model that answered."},
						{Name: "claude_code", Says: "The Claude Code that ran."},
						{Name: "permission_mode", Says: "The permission mode it ran in."},
						{Name: "denied", Says: "Each tool it reached for and was not allowed."},
						{Name: "cost_usd", Says: "What Claude Code says the turn cost."},
						{Name: "took_ms", Says: "How long the turn took."},
						{Name: "unwritten", Says: "Why a row of the session was not written down, when one was not."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/claude/say"},
				},
				{
					Name: "am",
					Does: "Who the ROOT agent is and how the node runs it.",
					Gives: []*protocol.Field{
						{Name: "did", Says: "Its own DID, which signs what it writes down."},
						{Name: "model", Says: "The model am.toml names."},
						{Name: "effort", Says: "The effort am.toml names."},
						{Name: "permission_mode", Says: "The permission mode it runs in when whoever speaks names none."},
						{Name: "permission_modes", Says: "Every permission mode Claude Code has."},
						{Name: "allow", Says: "The tools it may use without being asked."},
						{Name: "session", Says: "The session it continues, or empty before anything was said to it."},
						{Name: "claude_code", Says: "Where the Claude Code it runs on is, or empty when the node has none."},
						{Name: "not_ready", Says: "Why it cannot be spoken to, when it cannot."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/claude"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"say": s.claudeSay, "am": s.claudeAm},
	}
}

// spokenBy is who said it, as the node admitted them: a token by its DID, a
// person by the route they came in by. Empty on a node with no login.
func spokenBy(caller *http.Request) string {
	admitted, gated := auth.AdmissionFrom(caller.Context())
	switch {
	case !gated:
		return ""
	case admitted.Grant != nil:
		return admitted.Grant.DID
	case admitted.TokenDID != "":
		return admitted.TokenDID
	}
	return admitted.Identity
}

// thereIsNoRootAgent is why this node has no ROOT agent to speak to: am.toml
// names none, or the one it names did not start.
func (s *QNTXServer) thereIsNoRootAgent() *protocol.Refusal {
	if s.noRootAgent != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: "the ROOT agent am.toml names did not start: " + s.noRootAgent.Error()}
	}
	return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no ROOT agent ([agent.root]), so there is none to speak to"}
}

func (s *QNTXServer) claudeSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what is said to the ROOT agent is written where its caller stands, and this asking carried no request"}
	}
	agent := s.rootAgent
	if agent == nil {
		return nil, s.thereIsNoRootAgent()
	}
	named := s.deps.cfg.Agent.Root
	mode := sent["permission_mode"]
	if mode == "" {
		mode = named.Mode
	}
	if mode == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "permission_mode",
			Says: "no permission mode was named, and am.toml gives none under [agent.root]"}
	}
	store, err := s.storeFor(caller)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no namespace to write the session in: " + err.Error()}
	}

	select {
	case agent.turn <- struct{}{}:
		defer func() { <-agent.turn }()
	case <-ctx.Done():
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the ROOT agent was still answering somebody else when this caller left"}
	}

	binary, err := s.claudeCode.Path(ctx)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Claude Code to run: " + err.Error()}
	}
	plan, err := secretref.Resolve(ctx, named.TokenRef)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Claude plan token am.toml names did not resolve: " + err.Error()}
	}
	session, resumes, err := agent.session()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}

	// The session is the agent's to write down, signed as itself. A row that
	// does not land is said with the answer and never stops the turn.
	writes := sessionWriter{did: agent.did, session: session, resumed: resumes}
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
			s.logger.Errorw("a row of the ROOT agent's session was not written", "session", session, "error", err)
			if unwritten == "" {
				unwritten = err.Error()
			}
		}
	}
	told, err := writes.told(sent["says"], spokenBy(caller), time.Now())
	write([]*types.As{told}, err)

	said := claudecode.Said{
		Binary: binary, Home: agent.home, Session: session, Resumes: resumes,
		Says: sent["says"], Model: named.Model, Effort: named.Effort, Token: plan,
		System: agent.isSaidToBe(), Mode: mode, Allow: named.Allow,
	}
	if s.ownURL != "" {
		said.MCP = []claudecode.MCPServer{{Name: rootAgentMCP, URL: s.ownURL + "/mcp", Bearer: agent.token}}
	}
	// Under the node's own context and not the caller's: a caller that leaves
	// does not stop what it asked for halfway.
	answer, err := said.Run(s.ctx, func(m claudecode.Message) { write(writes.rowsOf(m, time.Now())) })
	if err != nil {
		write(writes.rowsOf(claudecode.Message{Type: "result", Subtype: "no_result", IsError: true, Result: err.Error()}, time.Now()))
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "Claude Code did not answer: " + err.Error()}
	}
	if !resumes {
		if err := agent.keep(session); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "it answered, and the session it answered in was not kept: " + err.Error()}
		}
	}

	denied := answer.Denied
	if denied == nil {
		denied = []string{}
	}
	return map[string]any{
		"answer": answer.Text, "is_error": answer.IsError, "subtype": answer.Subtype,
		"session": session, "model": answer.Model, "claude_code": answer.Version,
		"permission_mode": mode, "denied": denied,
		"cost_usd": answer.CostUSD, "took_ms": answer.Took.Milliseconds(), "unwritten": unwritten,
	}, nil
}

func (s *QNTXServer) claudeAm(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	agent := s.rootAgent
	if agent == nil {
		return nil, s.thereIsNoRootAgent()
	}
	named := s.deps.cfg.Agent.Root
	is := map[string]any{
		"did": agent.did, "model": named.Model, "effort": named.Effort,
		"permission_mode": named.Mode, "permission_modes": appcfg.PermissionModes,
		"allow": named.Allow, "session": "", "claude_code": "", "not_ready": "",
	}
	if is["allow"] == nil {
		is["allow"] = []string{}
	}
	session, resumes, err := agent.session()
	if err != nil {
		is["not_ready"] = err.Error()
	} else if resumes {
		is["session"] = session
	}
	// Asked without waiting: a fetch still going is said, not sat through.
	now, cancel := context.WithCancel(ctx)
	cancel()
	if path, err := s.claudeCode.Path(now); err != nil {
		is["not_ready"] = err.Error()
	} else {
		is["claude_code"] = path
	}
	return is, nil
}
