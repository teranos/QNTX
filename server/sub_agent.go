package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// agentSubsystem gets a node what the agents am.toml names run on (ADR-048).
type agentSubsystem struct{}

func (agentSubsystem) Name() string { return "agent" }

// harnessHeld is a pinned harness on this node, Claude Code or Pi: fetched
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

// holdPi builds the pinned Pi with the machine's nix, off the boot's own time:
// a build Nix does not hold yet takes minutes, and the node serves meanwhile.
func holdPi(ctx context.Context, flake string, spawn func(string, func()), logger *zap.SugaredLogger) *harnessHeld {
	held := &harnessHeld{name: "Pi", fetched: make(chan struct{})}
	spawn("agent.pi", func() {
		defer close(held.fetched)
		nix, err := pi.Nix()
		var home string
		if err == nil {
			home, err = os.UserHomeDir()
		}
		if err == nil {
			held.path, err = pi.Ensure(ctx, nix, flake, filepath.Join(home, ".qntx", "pi", "result"))
		}
		held.err = err
		if err != nil {
			logger.Errorw("Pi was not built, so the ROOT agent runs in Claude Code alone", "flake", flake, "error", err)
			return
		}
		logger.Infow("Pi is on this node", "version", pi.PinnedVersion, "path", held.path)
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
	defer func() { s.noRootAgent = err }()
	pin, err := claudecode.Pinned()
	if err != nil {
		return errors.Wrap(err, "the Claude Code this build pins did not read")
	}
	dir, err := claudeCodeDir()
	if err != nil {
		return err
	}
	s.holdHarness("claude", holdClaudeCode(s.ctx, pin, dir, s.wg.Go, s.logger))
	if s.deps.cfg.Agent.Root.Pi.Named() {
		s.holdHarness("pi", holdPi(s.ctx, pi.PinnedFlake, s.wg.Go, s.logger))
	}

	if s.turnsApart, err = claudecode.ApartHere(); err != nil {
		return err
	}

	home, err := rootAgentHome()
	if err != nil {
		return err
	}
	// Every agent the node runs is kept beside ROOT's (ADR-048).
	s.agentsDir = filepath.Dir(home)
	if err := s.nameRootAgent(home); err != nil {
		return err
	}
	agent := s.rootAgent
	s.wg.Go("agent.turnLeft", func() { s.takeUpTurnLeft(agent) })
	return nil
}

// nameRootAgent makes the ROOT agent this node's: a key of its own from the
// node's, and its token written down where the gate reads tokens. A node with
// no login has no gate to tell.
func (s *QNTXServer) nameRootAgent(home string) error {
	agent, err := theRootAgent(s.nodeDID.PrivateKey, home)
	if err != nil {
		return errors.Wrap(err, "the ROOT agent has no key of its own")
	}
	if s.authHandler != nil {
		if err := s.authHandler.HoldRootAgent(agent.token, agent.did); err != nil {
			return errors.Wrap(err, "the ROOT agent's token is not held, so it would reach none of the node's sigils")
		}
	}
	s.rootAgent = agent
	s.logger.Infow("The ROOT agent is this node's", "did", agent.did, "home", home)
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
