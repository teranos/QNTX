package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/claudecode"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
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
	// sessions is its session in each harness, by the harness's name: they
	// answer beside each other (ADR-048).
	sessions   map[string]*inHarness
	sessionsMu sync.Mutex
	// signingIn is its sign-in to Claude Code begun and not finished.
	signingIn signingIn
	// namespace is the namespace this agent stands in: system for ROOT's,
	// whose sessions are the node's own record (ADR-048).
	namespace string
	// called is the agent as a sentence names it.
	called string
}

// agentSpec is how an agent runs in Claude Code: am.toml's [agent.root] for
// ROOT's, and the namespace's AGENT line for a namespace agent.
type agentSpec struct {
	Model, Effort, Mode string
	Allow               []string
	// TokenRef is a reference to a Claude plan token, or empty for an agent on
	// its own sign-in (claude login).
	TokenRef string
}

// specOf is how am.toml says the ROOT agent runs in Claude Code.
func specOf(root appcfg.RootAgentConfig) agentSpec {
	return agentSpec{Model: root.Model, Effort: root.Effort, Mode: root.Mode, Allow: root.Allow, TokenRef: root.TokenRef}
}

// in is the agent's session in h.
func (a *rootAgent) in(h *harness) *inHarness {
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()
	if a.sessions == nil {
		a.sessions = map[string]*inHarness{}
	}
	held, ok := a.sessions[h.name]
	if !ok {
		held = keptIn(h.file)
		a.sessions[h.name] = held
	}
	return held
}

// inHarness is the ROOT agent's session in one harness: where it is kept, and
// the turn it is in.
type inHarness struct {
	// file is where the session is kept, under the agent's home.
	file string
	// turn holds one value while it is being spoken to: a session hears what
	// is said to it one at a time.
	turn chan struct{}
	// answering is the turn it is in, and nil between turns.
	answering atomic.Pointer[turnInSession]
}

func keptIn(file string) *inHarness {
	return &inHarness{file: file, turn: make(chan struct{}, 1)}
}

// The file each harness's session is kept in, under the agent's home.
const (
	claudeSessionFile = "session"
	piSessionFile     = "pi-session"
)

// turnInSession is a turn going on, by the session it is in.
type turnInSession struct{ session string }

// theRootAgent is the ROOT agent of the node holding this key, working in home.
func theRootAgent(node ed25519.PrivateKey, home string) (*rootAgent, error) {
	agent, err := theAgent(node, rootAgentPurpose, home)
	if err != nil {
		return nil, err
	}
	agent.namespace, agent.called = auth.NamespaceSystem, "the ROOT agent"
	return agent, nil
}

// theAgent is an agent of the node holding this key: its own key and token
// derived from the node's for purpose, so it is the same DID wherever the node
// is rebuilt from its record, working in home.
func theAgent(node ed25519.PrivateKey, purpose, home string) (*rootAgent, error) {
	key, err := access.DeriveKey(node, purpose)
	if err != nil {
		return nil, err
	}
	token, did, err := access.DeriveToken(node, purpose)
	if err != nil {
		return nil, err
	}
	return &rootAgent{did: did, signer: signing.NewSigner(key, did), token: token, home: home}, nil
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

// sessionIn is the session file keeps, or a new id when it keeps none yet.
func (a *rootAgent) sessionIn(file string) (id string, kept bool, err error) {
	held, err := os.ReadFile(filepath.Join(a.home, file))
	if os.IsNotExist(err) {
		return uuid.NewString(), false, nil
	}
	if err != nil {
		return "", false, errors.Wrapf(err, "the ROOT agent's session in %s did not read", file)
	}
	return strings.TrimSpace(string(held)), true, nil
}

// keepIn writes id down in file as the session continued from here on.
func (a *rootAgent) keepIn(file, id string) error {
	if err := os.MkdirAll(a.home, 0o700); err != nil {
		return errors.Wrapf(err, "could not create %s", a.home)
	}
	path := filepath.Join(a.home, file)
	return errors.Wrapf(os.WriteFile(path, []byte(id+"\n"), 0o600), "could not write %s", path)
}

// isSaidToBe is what the ROOT agent additionally is, said to Claude Code with
// everything said to it.
func (a *rootAgent) isSaidToBe() string {
	if a.namespace != auth.NamespaceSystem {
		return "You are the agent of the namespace " + a.namespace + " on a QNTX node, shared by everyone who has reach on that namespace. " +
			"Your DID is " + a.did + ". " +
			"The node's sigils are the tools of the MCP server named " + rootAgentMCP + ", which you reach with your own token; they act in " + a.namespace + " and nowhere else. " +
			"You hold no git and no credential of the node's: never ask for, read, print or store one."
	}
	return "You are the ROOT agent of a QNTX node: the node itself, as ROOT speaks to it. " +
		"You run on the machine the node runs on, as the user the node runs as. " +
		"Your DID is " + a.did + ". " +
		"The node's sigils are the tools of the MCP server named " + rootAgentMCP + ", which you reach with your own token. " +
		"Your git is your own: commits are authored as you, and when you push to GitHub your git asks the node for what carries the push. " +
		"Never ask for, read, print or store a GitHub credential yourself. " +
		"What GitHub answers over its API, a pull request opened or a run read, you ask with the github_ask tool; github_operations lists what it can be asked."
}

// claudeHarness is the ROOT agent in Claude Code.
func (s *QNTXServer) claudeHarness() *harness {
	named := func() appcfg.RootAgentConfig { return s.deps.cfg.Agent.Root }
	h := &harness{
		name: "claude", called: "Claude Code", file: claudeSessionFile,
		description: "The ROOT agent: Claude Code, run by the node as itself.",
		sayDoes:     "Says something to the ROOT agent and gives what it answered. It is one session that continues, written down by the agent as it goes and read with claude session.",
		amDoes:      "Who the ROOT agent is and how the node runs it.",
		sessionDoes: "The ROOT agent's one session, whole: everything said to it by whoever said it, as a transcript.",
		sayTakes: []*protocol.Param{
			{Name: "permission_mode", OneOf: appcfg.PermissionModes, Says: "The permission mode Claude Code runs this in. Not sent, it is the one am.toml gives."},
		},
		sayAnswer: "protocol.ClaudeSaid",
		amAnswer:  "protocol.ClaudeAm",
		// am.toml naming the ROOT agent names it in Claude Code.
		named:    func() bool { return true },
		absent:   s.thereIsNoRootAgent,
		fetching: "Claude Code is still being fetched",
		spec:     func() agentSpec { return specOf(named()) },
		part: func(ctx context.Context, sent sigil.Sent, agent *rootAgent, spec agentSpec) (aTurn, *protocol.Refusal) {
			return s.claudePart(spec, sent, agent)
		},
		am: func(is agentIn, agent *rootAgent, spec agentSpec) proto.Message {
			am := &protocol.ClaudeAm{
				Did: is.did, Model: spec.Model, Effort: spec.Effort, PermissionMode: spec.Mode,
				PermissionModes: appcfg.PermissionModes, Allow: spec.Allow,
				Session: is.session, Answering: is.answering, ClaudeCode: is.path, NotReady: is.notReady,
			}
			// Asked of Claude Code itself, when the node holds one.
			if binary, arrived, err := s.harnessHeldBy("claude").Now(); arrived && err == nil {
				if status, err := s.claudeStatus(s.ctx, binary, agent.home); err == nil {
					am.SignedIn, am.AuthMethod = status.SignedIn, status.AuthMethod
				}
			}
			return am
		},
	}
	h.also = []harnessSigil{s.loginSigil(h)}
	return h
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

// claudePart is Claude Code's part of one turn: run in the permission mode
// whoever speaks names, or the one am.toml gives.
func (s *QNTXServer) claudePart(named agentSpec, sent sigil.Sent, agent *rootAgent) (aTurn, *protocol.Refusal) {
	mode := sent["permission_mode"]
	if mode == "" {
		mode = named.Mode
	}
	if mode == "" {
		return aTurn{}, &protocol.Refusal{Why: sigil.Missing, Param: "permission_mode",
			Says: "no permission mode was named, and none is set for " + agent.called + " (am.toml gives it under [agent.root])"}
	}

	var binary, plan string
	return aTurn{
		effort: named.Effort,
		ready: func(ctx context.Context, session string, kept bool) (bool, *protocol.Refusal) {
			var err error
			if binary, err = s.harnessHeldBy("claude").Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no Claude Code to run: " + err.Error()}
			}
			if plan, err = secretref.Resolve(ctx, named.TokenRef); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "the Claude plan token am.toml names did not resolve: " + err.Error()}
			}
			// No plan token named is Claude Code on its own sign-in, which has
			// to have happened: a turn with no credential at all is not run.
			if plan == "" {
				status, err := s.claudeStatus(ctx, binary, agent.home)
				if err != nil {
					return false, &protocol.Refusal{Why: sigil.Failed, Says: "Claude Code did not say whether the agent is signed in: " + err.Error()}
				}
				if !status.SignedIn {
					return false, &protocol.Refusal{Why: sigil.Failed,
						Says: agent.called + " has no Claude credential: no plan token is named for it, and it is not signed in; sign it in with claude login"}
				}
			}
			// Pi may have started the session: Claude Code resumes only what it holds.
			return kept && claudecode.Holds(agent.home, session), nil
		},
		run: func(t turnRun) (proto.Message, error) {
			said := claudecode.Said{
				Binary: binary, Home: agent.home, Session: t.session, Resumes: t.resumes,
				Says: t.says, Model: named.Model, Effort: named.Effort, Token: plan,
				System: agent.isSaidToBe(), Mode: mode, Allow: named.Allow, Env: t.env,
			}
			if s.ownURL != "" {
				said.MCP = []claudecode.MCPServer{{Name: rootAgentMCP, URL: s.ownURL + "/mcp", Bearer: agent.token}}
			}
			answer, err := said.Run(s.ctx, func(m claudecode.Message) { t.write(t.writes.rowsOf(m, t.now())) })
			if err != nil {
				t.write(t.writes.rowsOf(claudecode.Message{Type: "result", Subtype: "no_result", IsError: true, Result: err.Error()}, t.now()))
				return nil, err
			}
			return &protocol.ClaudeSaid{
				Answer: answer.Text, IsError: answer.IsError, Subtype: answer.Subtype,
				Session: t.session, Model: answer.Model, ClaudeCode: answer.Version,
				PermissionMode: mode, Denied: answer.Denied,
				CostUsd: answer.CostUSD, TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
			}, nil
		},
	}, nil
}

// readAgentSession reads the session in keeps, or the one a turn going is in.
func (s *QNTXServer) readAgentSession(agent *rootAgent, in *inHarness) (any, *protocol.Refusal) {
	session, kept, err := agent.sessionIn(in.file)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if going := in.answering.Load(); going != nil {
		session, kept = going.session, true
	}
	return s.sessionTranscript(agent, session, kept)
}

// sessionTranscript is session read whole from where the agent writes it
// (sessionStoreOf), or empty before it was kept.
func (s *QNTXServer) sessionTranscript(agent *rootAgent, session string, resumes bool) (any, *protocol.Refusal) {
	none := transcript{Subjects: []string{}, Turns: []transcriptTurn{}}
	if !resumes {
		return &protocol.SessionTranscript{Transcript: none.message()}, nil
	}
	written, err := s.held.Read(agent.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no " + agent.namespace + " to read the session of " + agent.called + " from: " + err.Error()}
	}
	read, refused := sessionsIn(written, []string{session}, 1)
	if refused != nil {
		return nil, refused
	}
	if len(read) == 0 {
		none.Session = session
		return &protocol.SessionTranscript{Transcript: none.message()}, nil
	}
	return &protocol.SessionTranscript{Transcript: read[0].message()}, nil
}
