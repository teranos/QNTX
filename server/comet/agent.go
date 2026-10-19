package comet

// "to get ground setup for the ROOT agent"

// "AS AN A2A AGENT, IT ALSO NEEDS TO SHACKLE ITSELF UNDER GROUND"

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// AgentDeclaration is the agent's sand besides ground's own controls: its work directory as its
// one project, and the node it reports to, whose token ground reads from QNTX_TOKEN (ground source/attest.d).
func AgentDeclaration(work, url string) string {
	return "project {\n  path: \"" + work + "\"\n}\n\nqntx {\n  url: \"" + url + "\"\n}\n"
}

// AgentGround is where an agent's ground is kept: in its home, beside its Claude Code configuration.
func AgentGround(home string) string { return filepath.Join(home, "bin", "ground") }

// BuildAgent builds the ground an agent runs under, for the node's own platform, and keeps it in
// the agent's home: the agent's comet is the box's own (ADR-050, The ROOT agent).
func (b Builder) BuildAgent(ctx context.Context, home, url string) error {
	building.Lock()
	defer building.Unlock()
	platform, err := Native()
	if err != nil {
		return err
	}
	builds, err := b.Node.BuildsDir()
	if err != nil {
		return err
	}
	work := filepath.Join(builds, "comet-agent-"+filepath.Base(home))
	if err := os.RemoveAll(work); err != nil {
		return errors.Wrapf(err, "the last build of the ground of the agent in %s was not cleared", home)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s to build the agent's ground in", work)
	}
	sand := map[string]string{"comet.pbt": AgentDeclaration(filepath.Join(home, "work"), url)}
	return b.ground(ctx, work, platform, sand, "agent:"+filepath.Base(home), AgentGround(home))
}

// AgentPath is the PATH an agent's turn runs with: its own ground first, which its hooks and a
// ritual it performs run as ground.
func AgentPath(home, path string) string { return filepath.Dir(AgentGround(home)) + ":" + path }

// agentBuilds is what each agent's last ground build came to, by its home, until one stands.
var agentBuilds sync.Map

// GroundAgent builds the agent's ground apart from whoever asked, then writes the settings that run
// it; what the build came to is kept for Under to say.
func (b Builder) GroundAgent(ctx context.Context, home, url string) {
	agentBuilds.Store(home, "its ground is building since "+time.Now().UTC().Format(time.RFC3339))
	sacred.Go("comet.agent."+filepath.Base(home), func() {
		err := b.BuildAgent(ctx, home, url)
		if err == nil {
			err = AgentSettings(home)
		}
		if err != nil {
			agentBuilds.Store(home, "its last ground build failed: "+err.Error())
			return
		}
		agentBuilds.Delete(home)
	})
}

// Under is whether the agent in home runs under ground: its ground built, and its Claude Code's
// settings running it. Not under ground, it says why.
func Under(home string) error {
	why := "its ground is not built"
	if last, building := agentBuilds.Load(home); building {
		why = last.(string)
	}
	ground := AgentGround(home)
	info, err := os.Stat(ground)
	if err != nil {
		return errors.Wrapf(err, "the agent does not run under ground: %s", why)
	}
	const ownerRuns = 0o100
	if info.Mode().Perm()&ownerRuns != ownerRuns {
		return errors.Newf("the agent does not run under ground: %s is not executable", ground)
	}
	settings, err := os.ReadFile(filepath.Join(home, "claude", "settings.json"))
	if err != nil {
		return errors.Wrapf(err, "the agent does not run under ground: its settings are not written, and %s", why)
	}
	if !bytes.Contains(settings, []byte(ground)) {
		return errors.Newf("the agent does not run under ground: its settings run no %s", ground)
	}
	return nil
}

// agentHooks are the events ground answers, as the operator's own Claude Code wires them; sky is
// spawned on skyHooks. Under -p Claude Code waits for an asyncRewake hook to end, a day for sky,
// so an agent's sky is spawned apart: each streams what is pending on its pass (q, 2026-10-19).
var agentHooks = []string{"PreToolUse", "PostToolUse", "PreCompact", "Stop", "SessionStart", "UserPromptSubmit",
	"PostToolUseFailure", "Notification", "StopFailure", "SubagentStart", "SubagentStop", "TeammateIdle",
	"TaskCompleted", "InstructionsLoaded", "ConfigChange", "SessionEnd", "MessageDisplay"}

var skyHooks = map[string]bool{"PostToolUse": true, "Stop": true, "SessionStart": true}

// AgentSettings writes the hooks of the agent's Claude Code, under CLAUDE_CONFIG_DIR, each running its
// ground. What else the settings hold is kept.
func AgentSettings(home string) error {
	path := filepath.Join(home, "claude", "settings.json")
	settings := map[string]any{}
	held, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "the agent's settings at %s were not read", path)
	}
	if err == nil {
		if err := json.Unmarshal(held, &settings); err != nil {
			return errors.Wrapf(err, "the agent's settings at %s are not JSON", path)
		}
	}
	ground := AgentGround(home)
	hooks := map[string]any{}
	for _, event := range agentHooks {
		run := []any{map[string]any{"type": "command", "command": ground}}
		if skyHooks[event] {
			run = append(run, map[string]any{"type": "command", "command": ground + " sky \"$PWD\" </dev/null >/dev/null 2>&1 &"})
		}
		hooks[event] = []any{map[string]any{"matcher": "", "hooks": run}}
	}
	settings["hooks"] = hooks
	written, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return errors.Wrap(err, "the agent's settings did not marshal")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.Wrapf(err, "failed to make %s", filepath.Dir(path))
	}
	return errors.Wrapf(os.WriteFile(path, written, 0o600), "failed to write the agent's settings at %s", path)
}
