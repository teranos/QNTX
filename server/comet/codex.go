package comet

import (
	"os"
	"path/filepath"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// codexHooks are the events codex 0.163.0 sends that ground answers.
var codexHooks = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse",
	"SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "Stop", "SessionEnd"}

// AgentCodexConfig writes the agent's codex configuration, under CODEX_HOME: the
// node's gateway at gateway, the model's provider, reached with the key codex
// reads from CODEX_API_KEY, and the agent's ground_hook on every event codex sends.
func AgentCodexConfig(home, gateway string) error {
	var b strings.Builder
	b.WriteString("model_provider = \"qntx\"\n\n")
	b.WriteString("[model_providers.qntx]\nname = \"QNTX\"\n")
	b.WriteString("base_url = " + quoted(gateway) + "\n")
	b.WriteString("wire_api = \"responses\"\nenv_key = \"CODEX_API_KEY\"\n")
	hook := AgentHook(home)
	for _, event := range codexHooks {
		b.WriteString("\n[[hooks." + event + "]]\nhooks = [{ type = \"command\", command = " + quoted(hook) + " }]\n")
	}
	dir := filepath.Join(home, "codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.Wrapf(err, "failed to make the agent's CODEX_HOME %s", dir)
	}
	path := filepath.Join(dir, "config.toml")
	return errors.Wrapf(os.WriteFile(path, []byte(b.String()), 0o600), "failed to write the agent's codex configuration at %s", path)
}

// quoted is s as a TOML basic string.
func quoted(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s) + "\""
}
