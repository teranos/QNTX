package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	// turn holds one value while it is being spoken to in Claude Code: a
	// session hears what is said to it one at a time.
	turn chan struct{}
	// answering is the turn it is in, and nil between turns.
	answering atomic.Pointer[turnInSession]
	// piTurn and piAnswering are the same of its session in Pi, which answers
	// beside the one in Claude Code (ADR-048).
	piTurn      chan struct{}
	piAnswering atomic.Pointer[turnInSession]
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
	key, err := access.DeriveKey(node, rootAgentPurpose)
	if err != nil {
		return nil, err
	}
	token, did, err := access.DeriveToken(node, rootAgentPurpose)
	if err != nil {
		return nil, err
	}
	return &rootAgent{did: did, signer: signing.NewSigner(key, did), token: token, home: home,
		turn: make(chan struct{}, 1), piTurn: make(chan struct{}, 1)}, nil
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

// session is the session it continues in Claude Code, and whether it was kept
// already. Before anything was said to it, it is a new id.
func (a *rootAgent) session() (id string, kept bool, err error) {
	return a.sessionIn(claudeSessionFile)
}

// keep writes down the session it continues in Claude Code from here on.
func (a *rootAgent) keep(id string) error {
	return a.keepIn(claudeSessionFile, id)
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
	return "You are the ROOT agent of a QNTX node: the node itself, as ROOT speaks to it. " +
		"You run on the machine the node runs on, as the user the node runs as. " +
		"Your DID is " + a.did + ". " +
		"The node's sigils are the tools of the MCP server named " + rootAgentMCP + ", which you reach with your own token. " +
		"Your git is your own: commits are authored as you, and when you push to GitHub your git asks the node for what carries the push. " +
		"Never ask for, read, print or store a GitHub credential yourself. " +
		"What GitHub answers over its API, a pull request opened or a run read, you ask with the github_ask tool; github_operations lists what it can be asked."
}

func (s *QNTXServer) claudeSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "claude",
			Description: "The ROOT agent: Claude Code, run by the node as itself.",
			Tags:        []string{"agent", "claude", "root"},
			Sigils: []*protocol.Sigil{
				{
					Name: "say",
					Does: "Says something to the ROOT agent and gives what it answered. It is one session that continues, written down by the agent as it goes and read with claude session.",
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
						{Name: "answering", Says: "Whether it is in a turn now."},
						{Name: "claude_code", Says: "Where the Claude Code it runs on is, or empty when the node has none."},
						{Name: "not_ready", Says: "Why it cannot be spoken to, when it cannot."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/claude"},
				},
				{
					Name: "session",
					Does: "The ROOT agent's one session, whole: everything said to it by whoever said it, as a transcript.",
					Gives: []*protocol.Field{
						{Name: "transcript", Says: "Its session as turns, each naming the attestation it was read from. Empty before anything was said to it.", Message: "protocol.Transcript"},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/claude/session"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"say": s.claudeSay, "am": s.claudeAm, "session": s.claudeSession},
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
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what is said to the ROOT agent is written down with who said it, and this asking carried no request"}
	}
	agent := s.rootAgent
	if agent == nil {
		return nil, s.thereIsNoRootAgent()
	}
	// Its token is ROOT's kind, and it would be asking from inside the turn
	// it then waits on.
	if spokenBy(caller) == agent.did {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: "the ROOT agent does not speak to itself: it is in the turn that asked"}
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
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no system to write the ROOT agent's session in: " + err.Error()}
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
	session, kept, err := agent.session()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	// Pi may have started the session: Claude Code resumes only what it holds.
	resumes := kept && claudecode.Holds(agent.home, session)

	agent.answering.Store(&turnInSession{session: session})
	defer agent.answering.Store(nil)

	// The session is the agent's to write down, signed as itself. A row that
	// does not land is said with the answer and never stops the turn.
	writes := sessionWriter{did: agent.did, session: session, resumed: resumes, effort: named.Effort}
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
	itsGit, err := s.gitEnvironment(agent)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the ROOT agent's git was not set up, so nothing was said to it: " + err.Error()}
	}
	told, err := writes.told(sent["says"], spokenBy(caller), time.Now())
	write([]*types.As{told}, err)

	said := claudecode.Said{
		Binary: binary, Home: agent.home, Session: session, Resumes: resumes,
		Says: sent["says"], Model: named.Model, Effort: named.Effort, Token: plan,
		System: agent.isSaidToBe(), Mode: mode, Allow: named.Allow, Env: itsGit,
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
	if !kept {
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

// claudeSession reads the session from where it is written, so whoever may
// talk to it reads all of it, wherever they stand.
func (s *QNTXServer) claudeSession(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	agent := s.rootAgent
	if agent == nil {
		return nil, s.thereIsNoRootAgent()
	}
	return s.readAgentSession(agent, claudeSessionFile, agent.answering.Load())
}

// readAgentSession reads the session file keeps, or the one a turn going is in.
func (s *QNTXServer) readAgentSession(agent *rootAgent, file string, going *turnInSession) (any, *protocol.Refusal) {
	session, kept, err := agent.sessionIn(file)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if going != nil {
		session, kept = going.session, true
	}
	return s.sessionTranscript(session, kept)
}

// sessionTranscript is session read whole, or empty before it was kept.
func (s *QNTXServer) sessionTranscript(session string, resumes bool) (any, *protocol.Refusal) {
	none := transcript{Subjects: []string{}, Turns: []transcriptTurn{}}
	if !resumes {
		return map[string]any{"transcript": none}, nil
	}
	system, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no system to read the ROOT agent's session from: " + err.Error()}
	}
	read, refused := sessionsIn(system, []string{session}, 1)
	if refused != nil {
		return nil, refused
	}
	if len(read) == 0 {
		none.Session = session
		return map[string]any{"transcript": none}, nil
	}
	return map[string]any{"transcript": read[0]}, nil
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
		"allow": named.Allow, "session": "", "answering": false, "claude_code": "", "not_ready": "",
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
	// A first turn is in a session not kept yet, and is read all the same.
	if going := agent.answering.Load(); going != nil {
		is["answering"], is["session"] = true, going.session
	}
	// Asked without waiting: a fetch still going is said, not sat through.
	switch path, arrived, err := s.claudeCode.Now(); {
	case !arrived:
		is["not_ready"] = "Claude Code is still being fetched"
	case err != nil:
		is["not_ready"] = err.Error()
	default:
		is["claude_code"] = path
	}
	return is, nil
}
