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

// AgentGrove is the agent's checkout of sbvh-nl/grove, where its work keeps each repository as
// work/<owner>/<repo> (AgentTrust): what a grove ritual needs stands there (ADR-050, Not done).
func AgentGrove(home string) string { return filepath.Join(home, "work", "sbvh-nl", "grove") }

// AgentSand is the agent's sand besides ground's own controls: its declaration, and each pbt of its
// grove checkout's controls/, which declare grove's rituals. An agent with no grove checkout has none.
func AgentSand(home, url string) (map[string]string, error) {
	sand := map[string]string{"comet.pbt": AgentDeclaration(filepath.Join(home, "work"), url)}
	controls := filepath.Join(AgentGrove(home), "controls")
	found, err := filepath.Glob(filepath.Join(controls, "*.pbt"))
	if err != nil {
		return nil, errors.Wrapf(err, "the controls of the agent's grove checkout in %s were not listed", controls)
	}
	for _, path := range found {
		pbt, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "the control %s of the agent's grove checkout was not read", path)
		}
		sand["grove-"+filepath.Base(path)] = string(pbt)
	}
	return sand, nil
}

// AgentLocalSand is where the agent's ground holds a local sand: under the HOME its turn carries,
// which is the node's (internal/claudecode run.go, agentenv), as ground's localSandPathInto names it.
func AgentLocalSand(platform string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "the HOME the agent's turn carries was not found, so its sand has nowhere to be held from")
	}
	return filepath.Join(home, ".local", "share", "ground", "sand", SandFile(platform)), nil
}

// AgentGround is where an agent's ground is kept: in its home, beside its Claude Code configuration.
func AgentGround(home string) string { return filepath.Join(home, "bin", "ground") }

// AgentHook is where the binary that answers the agent's hooks is kept, beside its ground.
func AgentHook(home string) string { return filepath.Join(home, "bin", "ground_hook") }

// AgentSky is where the agent's courier is kept, beside its ground.
func AgentSky(home string) string { return filepath.Join(home, "bin", "ground_sky") }

// AgentDrive is where the driver of a ritual the agent performs is kept: beside the ground that started it.
func AgentDrive(home string) string { return filepath.Join(home, "bin", "ground_drive") }

// BuildAgent builds the ground an agent runs under, for the node's own platform, and keeps it in
// the agent's home: the agent's comet is the box's own (ADR-050, The ROOT agent).
func (b Builder) BuildAgent(ctx context.Context, home, url string) error {
	building.Lock()
	defer building.Unlock()
	// An agent's ground runs on the node that builds it: natively.
	native, err := Native()
	if err != nil {
		return err
	}
	sand, err := AgentSand(home, url)
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
	kept := map[string]string{"ground": AgentGround(home), "ground_hook": AgentHook(home),
		"ground_sky": AgentSky(home), "ground_drive": AgentDrive(home)}
	src, env, err := b.ground(ctx, work, sand, "agent:"+filepath.Base(home), kept)
	if err != nil || len(sand) == 1 {
		return err
	}
	// The engine is built from ground's floor alone (ground dub.json -d-version=Floor): the rituals
	// reach it as the sand it holds beside it (ground source/sand/load.d holdLanded).
	if _, err := b.sandIn(ctx, src, env, native); err != nil {
		return errors.Wrap(err, "the sand that declares the agent's rituals did not build")
	}
	held, err := AgentLocalSand(native)
	if err != nil {
		return err
	}
	return keepBuilt(src, filepath.Join("build", SandFile(native)), held)
}

// AgentPath is the PATH an agent's turn runs with: its own ground first, which its hooks and a
// ritual it performs run as ground.
func AgentPath(home, path string) string { return filepath.Dir(AgentGround(home)) + ":" + path }

// agentBuilds is what each agent's last ground build came to, by its home, until one stands.
var agentBuilds sync.Map

// GroundAgent builds the agent's ground apart from whoever asked, keeps what it loads from as keep
// says, then writes the settings that run it; what the build came to is kept for Under to say, and
// said by log as it ends.
func (b Builder) GroundAgent(ctx context.Context, home, url string, keep func(home string) error, log func(msg string, keysAndValues ...any)) {
	agentBuilds.Store(home, "its ground is building since "+time.Now().UTC().Format(time.RFC3339))
	sacred.Go("comet.agent."+filepath.Base(home), func() {
		err := b.BuildAgent(ctx, home, url)
		if err == nil {
			err = keep(home)
		}
		if err == nil {
			err = AgentSettings(home)
		}
		if err != nil {
			agentBuilds.Store(home, "its last ground build failed: "+err.Error())
			log("An agent's ground build failed", "home", home, "error", err)
			return
		}
		agentBuilds.Delete(home)
		log("An agent's ground is built", "home", home, "ground", AgentGround(home))
	})
}

// Under is whether the agent in home runs under ground: its ground built, and its Claude Code's
// settings running it. Not under ground, it says why.
func Under(home string) error {
	why := "its ground is not built"
	if last, building := agentBuilds.Load(home); building {
		why = last.(string)
	}
	for _, built := range []string{AgentGround(home), AgentHook(home), AgentSky(home)} {
		info, err := os.Stat(built)
		if err != nil {
			return errors.Wrapf(err, "the agent does not run under ground: %s", why)
		}
		const ownerRuns = 0o100
		if info.Mode().Perm()&ownerRuns != ownerRuns {
			return errors.Newf("the agent does not run under ground: %s is not executable", built)
		}
	}
	settings, err := os.ReadFile(filepath.Join(home, "claude", "settings.json"))
	if err != nil {
		return errors.Wrapf(err, "the agent does not run under ground: its settings are not written, and %s", why)
	}
	hook := AgentHook(home)
	if !bytes.Contains(settings, []byte(hook)) {
		return errors.Newf("the agent does not run under ground: its settings run no %s", hook)
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
// ground_hook, and its courier ground_sky. What else the settings hold is kept.
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
	hooks := map[string]any{}
	for _, event := range agentHooks {
		run := []any{map[string]any{"type": "command", "command": AgentHook(home)}}
		if skyHooks[event] {
			// The event goes with it on a descriptor kept before the &: sh hands a command put in the
			// background /dev/null first, so under dash even <&0 named /dev/null.
			run = append(run, map[string]any{"type": "command", "command": "exec 3<&0; " + AgentSky(home) + " \"$PWD\" <&3 >/dev/null 2>&1 &"})
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
	if err := os.WriteFile(path, written, 0o600); err != nil {
		return errors.Wrapf(err, "failed to write the agent's settings at %s", path)
	}
	return AgentTrust(home)
}

// AgentTrust trusts the agent's work directory in its Claude Code, and each repository it keeps there
// as work/<owner>/<repo>: a ritual starts its own agent with claude --bg, which refuses a workspace
// nobody trusted, and a git checkout is trusted only by name, never by a parent's trust.
func AgentTrust(home string) error {
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return errors.Wrapf(err, "failed to make the agent's work directory %s", work)
	}
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		return errors.Wrapf(err, "the agent's work directory %s did not resolve", work)
	}
	trusted := []string{resolved}
	repos, err := filepath.Glob(filepath.Join(resolved, "*", "*", ".git"))
	if err != nil {
		return errors.Wrapf(err, "the repositories under %s were not listed", resolved)
	}
	for _, git := range repos {
		trusted = append(trusted, filepath.Dir(git))
	}
	path := filepath.Join(home, "claude", ".claude.json")
	config := map[string]any{}
	held, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "the agent's Claude Code configuration at %s was not read", path)
	}
	if err == nil {
		if err := json.Unmarshal(held, &config); err != nil {
			return errors.Wrapf(err, "the agent's Claude Code configuration at %s is not JSON", path)
		}
	}
	projects, ok := config["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
	}
	for _, dir := range trusted {
		project, ok := projects[dir].(map[string]any)
		if !ok {
			project = map[string]any{}
		}
		project["hasTrustDialogAccepted"] = true
		projects[dir] = project
	}
	config["projects"] = projects
	written, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return errors.Wrap(err, "the agent's Claude Code configuration did not marshal")
	}
	return errors.Wrapf(os.WriteFile(path, written, 0o600), "failed to write the agent's Claude Code configuration at %s", path)
}
