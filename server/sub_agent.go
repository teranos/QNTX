package server

import (
	"context"
	"os"
	"path/filepath"

	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// agentSubsystem gets a node what the agents am.toml names run on (ADR-048).
type agentSubsystem struct{}

func (agentSubsystem) Name() string { return "agent" }

// claudeCodeHeld is the pinned Claude Code on this node: fetched once at
// start, and asked for by whatever runs it.
type claudeCodeHeld struct {
	fetched chan struct{}
	path    string
	err     error
}

// Path is where the pinned binary is, or why this node has none. It waits for
// a fetch still going, and no longer than whoever asks.
func (h *claudeCodeHeld) Path(ctx context.Context) (string, error) {
	select {
	case <-h.fetched:
		return h.path, h.err
	case <-ctx.Done():
		return "", errors.Wrap(ctx.Err(), "Claude Code is still being fetched")
	}
}

// holdClaudeCode fetches what pin names into dir, off the boot's own time: the
// binary is hundreds of megabytes and the node serves while it arrives.
func holdClaudeCode(ctx context.Context, pin claudecode.Pin, dir string, spawn func(string, func()), logger *zap.SugaredLogger) *claudeCodeHeld {
	held := &claudeCodeHeld{fetched: make(chan struct{})}
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

func (agentSubsystem) Init(s *QNTXServer) error {
	if !s.deps.cfg.Agent.Root.Named() {
		return nil
	}
	pin, err := claudecode.Pinned()
	if err != nil {
		return errors.Wrap(err, "the Claude Code this build pins did not read")
	}
	dir, err := claudeCodeDir()
	if err != nil {
		return err
	}
	s.claudeCode = holdClaudeCode(s.ctx, pin, dir, s.wg.Go, s.logger)
	return nil
}
