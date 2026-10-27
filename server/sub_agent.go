package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/teranos/QNTX/internal/claudecode"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// agentSubsystem gets a node what the agents am.toml names run on (ADR-048).
type agentSubsystem struct{}

func (agentSubsystem) Name() string { return "agent" }

// harnessHeld is a pinned Coding Agent Harness on this node: fetched
// once at start, and asked for by whatever runs it.
type harnessHeld struct {
	name    string
	fetched chan struct{}
	path    string
	err     error
}

// called is the harness's name, for a sentence about it.
func (h *harnessHeld) called() string {
	if h.name == "" {
		return "Claude Code"
	}
	return h.name
}

// Path is where the pinned binary is, or why this node has none. It waits for
// a fetch still going, and no longer than whoever asks.
func (h *harnessHeld) Path(ctx context.Context) (string, error) {
	// What has arrived is said first: a select between two things that are
	// both ready picks either.
	if path, arrived, err := h.Now(); arrived {
		return path, err
	}
	select {
	case <-h.fetched:
		return h.path, h.err
	case <-ctx.Done():
		return "", errors.Wrapf(ctx.Err(), "%s is still being fetched", h.called())
	}
}

// Now is where the pinned binary is if the fetch has ended, without waiting
// on one still going.
func (h *harnessHeld) Now() (path string, arrived bool, err error) {
	select {
	case <-h.fetched:
		return h.path, true, h.err
	default:
		return "", false, nil
	}
}

// holdClaudeCode fetches what pin names into dir, off the boot's own time: the
// binary is hundreds of megabytes and the node serves while it arrives.
func holdClaudeCode(ctx context.Context, pin claudecode.Pin, dir string, spawn func(string, func()), logger *zap.SugaredLogger) *harnessHeld {
	held := &harnessHeld{name: "Claude Code", fetched: make(chan struct{})}
	spawn("agent.claudeCode", func() {
		defer close(held.fetched)
		held.path, held.err = pin.Ensure(ctx, dir)
		if held.err != nil {
			logger.Errorw("Claude Code was not fetched, so no agent this node names can run",
				"version", pin.Version, "dir", dir, "error", held.err)
			return
		}
		logger.Infow("Claude Code is on this node", "version", pin.Version, "path", held.path)
	})
	return held
}

// claudeCodeDir is where a node keeps the Claude Code it fetched, beside where
// it keeps the plugins it fetched.
func claudeCodeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve the home directory Claude Code is kept under")
	}
	return filepath.Join(home, ".qntx", "claude-code"), nil
}

func (agentSubsystem) Init(s *QNTXServer) (err error) {
	if !s.deps.cfg.Agent.Root.Named() {
		return nil
	}
	// The node serves without its agent, so why there is none is kept for
	// whoever speaks to it.
	defer func() { s.noNodeAgent = err }()
	pin, err := claudecode.Pinned()
	if err != nil {
		return errors.Wrap(err, "the Claude Code this build pins did not read")
	}
	dir, err := claudeCodeDir()
	if err != nil {
		return err
	}
	s.holdHarness("claude", holdClaudeCode(s.ctx, pin, dir, s.wg.Go, s.logger))
	if s.deps.cfg.Agent.Root.Codex.Named() {
		s.holdHarness("codex", holdCodex(s.ctx, filepath.Join(filepath.Dir(dir), "codex"), s.wg.Go, s.logger))
	}
	// No node holds Pi, so neither the Node Agent nor a namespace's agent runs in it:
	// "disable Pi tmp as a Namespace Agent and as a Node Agent"

	home, err := nodeAgentHome()
	if err != nil {
		return err
	}
	// Every agent the node runs is kept beside ROOT's (ADR-048).
	s.agentsDir = filepath.Dir(home)
	if err := s.nameNodeAgent(home); err != nil {
		return err
	}
	// The Node Agent's turns run apart from the node from its boot on: "on a
	// redeploy NODE agent would still be alive" (#1256).
	s.nodeAgentApart = true
	// The session is held from here, before anything can be said to it, until
	// the turns the node's last process left are read on.
	agent := s.nodeAgent
	in := agent.in(s.claudeHarness())
	in.turn <- struct{}{}
	effort := s.deps.cfg.Agent.Root.Effort
	// A Task is settled by this node only when the node before it held it.
	since := time.Now()
	s.wg.Go("agent.follow", func() { s.followLeftTurns(agent, in, effort, since) })
	return nil
}

// nameNodeAgent makes the Node Agent this node's: a key of its own from the
// node's, and its token written down where the gate reads tokens. A node with
// no login has no gate to tell.
func (s *QNTXServer) nameNodeAgent(home string) error {
	agent, err := theNodeAgent(s.nodeDID.PrivateKey, home)
	if err != nil {
		return errors.Wrap(err, "the Node Agent has no key of its own")
	}
	if s.authHandler != nil {
		if err := s.authHandler.HoldNodeAgent(agent.token, agent.did); err != nil {
			return errors.Wrap(err, "the Node Agent's token is not held, so it would reach none of the node's sigils")
		}
	}
	s.nodeAgent = agent
	s.underGround.add(home)
	s.logger.Infow("The Node Agent is this node's", "did", agent.did, "home", home)
	return nil
}

// ownURLOf is where a node bound to bind answers on its own machine: an
// address that means every interface is reached on loopback.
func ownURLOf(bind string, port int) string {
	switch bind {
	case "", "0.0.0.0":
		bind = "127.0.0.1"
	case "::":
		bind = "::1"
	}
	return "http://" + net.JoinHostPort(bind, strconv.Itoa(port))
}
