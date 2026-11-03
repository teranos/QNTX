package comet

import (
	"encoding/json"
	"os"
	"path/filepath"

	errors "github.com/teranos/sacred-error"
)

// agyHooks are the events agy sends, each of which ground answers as
// `ground_hook agy <Event>`; run as `agy acp`, 1.4.12 ran it for SessionStart,
// PreInvocation and Stop of a turn that called no tool. A tool's events take matcher groups; the others
// take their commands directly (antigravity.google/docs/hooks).
var agyHooks = []string{"SessionStart", "PreToolUse", "PostToolUse", "PreInvocation", "PostInvocation", "Stop"}

var agyToolHooks = map[string]bool{"PreToolUse": true, "PostToolUse": true}

// AgentAgyHooks writes the agent's agy hooks, in the global hooks.json under
// its HOME: one hook, ground, running the agent's ground_hook on every event
// agy sends. Other hooks the file names are kept.
func AgentAgyHooks(home string) error {
	path := filepath.Join(home, ".gemini", "config", "hooks.json")
	hooks := map[string]any{}
	held, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "the agent's agy hooks at %s were not read", path)
	}
	if err == nil {
		if err := json.Unmarshal(held, &hooks); err != nil {
			return errors.Wrapf(err, "the agent's agy hooks at %s are not JSON", path)
		}
	}
	ground := map[string]any{}
	for _, event := range agyHooks {
		run := map[string]any{"type": "command", "command": AgentHook(home) + " agy " + event}
		if agyToolHooks[event] {
			ground[event] = []any{map[string]any{"matcher": "", "hooks": []any{run}}}
		} else {
			ground[event] = []any{run}
		}
	}
	hooks["ground"] = ground
	written, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return errors.Wrap(err, "the agent's agy hooks did not marshal")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.Wrapf(err, "failed to make %s", filepath.Dir(path))
	}
	return errors.Wrapf(os.WriteFile(path, written, 0o600), "failed to write the agent's agy hooks at %s", path)
}
