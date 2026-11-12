package config

import (
	"strings"
	"testing"
)

// The Node Agent as am.toml names it (ADR-048): which model, at what effort,
// and where the Claude plan token is kept.
func TestNodeAgentLoads(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model  = "claude-opus-5-5"
effort = "low"
token  = "ssm:///q/box/claude/oauth-token"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	root := cfg.Agent.Root.Claude
	if root.Model != "claude-opus-5-5" || root.Effort != "low" || root.TokenRef != "ssm:///q/box/claude/oauth-token" {
		t.Errorf("the Node Agent loaded as %+v", root)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// What it runs in when whoever speaks to it names no mode, and the tools it
// may use without being asked, are am.toml's to say.
func TestNodeAgentPermissionsLoad(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model           = "claude-opus-5-5"
effort          = "low"
permission_mode = "dontAsk"
allow           = ["mcp__qntx", "Bash", "Read"]
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	root := cfg.Agent.Root.Claude
	if root.Mode != "dontAsk" || len(root.Allow) != 3 || root.Allow[0] != "mcp__qntx" {
		t.Errorf("the Node Agent's permissions loaded as mode %q, allow %v", root.Mode, root.Allow)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// A mode Claude Code does not have is a typo, and a typo that loaded would be
// an agent that fails at the first thing said to it.
func TestNodeAgentModeClaudeCodeDoesNotHaveIsRefused(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model           = "claude-opus-5-5"
effort          = "low"
permission_mode = "everything"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "agent.root.claude.permission_mode") || !strings.Contains(err.Error(), "dontAsk") {
		t.Errorf("Validate = %v, want it to name the field and the modes there are", err)
	}
}

// A node that names no Node Agent has none, and that is valid.
func TestNoNodeAgentIsValid(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, ``))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Agent.Root.Named() {
		t.Errorf("a node that names no Node Agent has one: %+v", cfg.Agent.Root)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// A literal token in a world-readable am.toml is already disclosed.
func TestNodeAgentTokenLiteralRejected(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model  = "claude-opus-5-5"
effort = "low"
token  = "sk-ant-oat01-a-literal"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate = nil, want the literal token refused")
	}
	if !strings.Contains(err.Error(), "agent.root.claude.token") {
		t.Errorf("error does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "sk-ant-oat01-a-literal") {
		t.Errorf("the error carries the literal it refused: %v", err)
	}
}

// Nothing stands in for the model or the effort: an agent named by half is
// refused, saying which half is missing.
func TestNodeAgentNamedByHalfIsRefused(t *testing.T) {
	for missing, body := range map[string]string{
		"agent.root.claude.model":  "[agent.root.claude]\neffort = \"low\"\n",
		"agent.root.claude.effort": "[agent.root.claude]\nmodel = \"claude-opus-5-5\"\n",
	} {
		cfg, err := LoadFromFile(writeConfig(t, body))
		if err != nil {
			t.Fatalf("LoadFromFile = %v", err)
		}
		err = cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("Validate = %v, want it to name %s", err, missing)
		}
	}
}

// The other harness it runs in: the model Pi runs, at what thinking, and the
// gateway plugin every model call goes through.
func TestNodeAgentPiLoads(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model  = "claude-opus-5-5"
effort = "low"

[agent.root.pi]
model    = "anthropic/claude-opus-4.6"
thinking = "low"
gateway  = "openrouter-qntx"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	pi := cfg.Agent.Root.Pi
	if !pi.Named() || pi.Model != "anthropic/claude-opus-4.6" || pi.Thinking != "low" || pi.Gateway != "openrouter-qntx" {
		t.Errorf("Pi loaded as %+v", pi)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// Pi named by half, or at a thinking level it does not have, is refused.
func TestNodeAgentPiNamedByHalfIsRefused(t *testing.T) {
	for want, body := range map[string]string{
		"both or neither":        "[agent.root.pi]\nmodel = \"m\"\n",
		"agent.root.pi.thinking": "[agent.root.pi]\nmodel = \"claude-opus-5-5\"\ngateway = \"g\"\nthinking = \"loud\"\n",
	} {
		cfg, err := LoadFromFile(writeConfig(t, "[agent.root.claude]\nmodel = \"claude-opus-5-5\"\neffort = \"low\"\n"+body))
		if err != nil {
			t.Fatalf("LoadFromFile = %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate = %v, want it to say %s", err, want)
		}
	}
}

// "any model that isn't Opus or Fable shouldn't be allowed to be NODE agent"
func TestNodeAgentOtherThanOpusOrFableIsRefused(t *testing.T) {
	for want, body := range map[string]string{
		"agent.root.claude.model": "[agent.root.claude]\nmodel = \"claude-sonnet-5-5\"\neffort = \"low\"\n",
		"agent.root.pi.model":     "[agent.root.claude]\nmodel = \"claude-opus-5-5\"\neffort = \"low\"\n[agent.root.pi]\nmodel = \"openai/gpt-oss-120b\"\ngateway = \"g\"\n",
	} {
		cfg, err := LoadFromFile(writeConfig(t, body))
		if err != nil {
			t.Fatalf("LoadFromFile = %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate = %v, want it to refuse %s", err, want)
		}
	}
	for _, model := range []string{"claude-opus-5-5", "claude-fable-5-1", "anthropic/claude-opus-4.6"} {
		if !NodeAgentModel(model) {
			t.Errorf("NodeAgentModel(%q) = false, want true", model)
		}
	}
}

// 「[Opus 5.5] "wiring it into node and namespace config as an actual harness": agy runs」
// 「[Opus 5.5] the model am.toml names. Asked whether the Node Agent in agy is held to Opus」
// 「[Opus 5.5] or Fable, which agy never serves: "Exempt agy".」
func TestNodeAgentAgyLoads(t *testing.T) {
	path := writeConfig(t, `
[agent.root.claude]
model  = "claude-opus-5-5"
effort = "low"

[agent.root.agy]
model = "gemini-3.8-flash-low"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	agy := cfg.Agent.Root.Agy
	if !agy.Named() || agy.Model != "gemini-3.8-flash-low" {
		t.Errorf("agy loaded as %+v", agy)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// "Let's say that I don't like how it's putting Claude on a pedestal."
//
// 「[Opus 5.5] A Node Agent named in Pi alone is a Node Agent: Claude Code is one
// harness among the others, not the agent itself.」
func TestNodeAgentInPiAloneIsNamed(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[agent.root.pi]
model   = "anthropic/claude-opus-4.6"
gateway = "openrouter-qntx"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if !cfg.Agent.Root.Named() || cfg.Agent.Root.Claude.Named() {
		t.Errorf("a Node Agent in Pi alone loaded as %+v", cfg.Agent.Root)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// 「[Opus 5.5] Claude Code named the way it was before [agent.root.claude] is read
// as [agent.root.claude], and the keys that moved are kept so the node says so.」
// "Read them as Claude, warn"
func TestClaudeNamedTheOldWayIsReadAsClaude(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[agent.root]
model           = "claude-opus-5-5"
effort          = "low"
permission_mode = "dontAsk"

[agent.root.pi]
model   = "anthropic/claude-opus-4.6"
gateway = "openrouter-qntx"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	claude := cfg.Agent.Root.Claude
	if claude.Model != "claude-opus-5-5" || claude.Effort != "low" || claude.Mode != "dontAsk" {
		t.Errorf("Claude Code named the old way loaded as %+v", claude)
	}
	if got := strings.Join(cfg.Agent.Root.MovedKeys(), " "); got != "agent.root.model agent.root.effort agent.root.permission_mode" {
		t.Errorf("MovedKeys = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}

	// Half named the old way, the old key is the one said to be missing.
	cfg, err = LoadFromFile(writeConfig(t, "[agent.root]\nmodel = \"claude-opus-5-5\"\n"))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.root.effort is required") {
		t.Errorf("Validate = %v, want it to name agent.root.effort", err)
	}
}

// 「[Opus 5.5] Named both ways, which one is meant is not guessed.」
func TestClaudeNamedBothWaysIsRefused(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[agent.root]
model = "claude-opus-5-5"

[agent.root.claude]
model  = "claude-fable-5-1"
effort = "low"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Agent.Root.Claude.Model != "claude-fable-5-1" {
		t.Errorf("[agent.root.claude] did not stand: %+v", cfg.Agent.Root.Claude)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.root.model") || !strings.Contains(err.Error(), "[agent.root.claude] alone") {
		t.Errorf("Validate = %v, want it to name agent.root.model and say where Claude Code is named", err)
	}
}

// "Incomplete Codex settings start the node" — "Yes"
//
// 「[Opus 5.5] Codex named by half, or in a sandbox codex has not, is refused at
// load, as codex itself would refuse it at the first turn.」
func TestNodeAgentCodexNamedByHalfIsRefused(t *testing.T) {
	for want, body := range map[string]string{
		"both or neither":           "[agent.root.codex]\ngateway = \"g\"\nsandbox = \"read-only\"\n",
		"agent.root.codex.sandbox":  "[agent.root.codex]\nmodel = \"m\"\ngateway = \"g\"\n",
		"agent.root.codex.sandbox ": "[agent.root.codex]\nmodel = \"m\"\ngateway = \"g\"\nsandbox = \"everything\"\n",
		"agent.root.codex.token":    "[agent.root.codex]\nmodel = \"m\"\ngateway = \"g\"\nsandbox = \"read-only\"\ntoken = \"a-literal\"\n",
	} {
		cfg, err := LoadFromFile(writeConfig(t, body))
		if err != nil {
			t.Fatalf("LoadFromFile = %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), strings.TrimSpace(want)) {
			t.Errorf("Validate = %v, want it to say %s", err, want)
		}
	}
	cfg, err := LoadFromFile(writeConfig(t, "[agent.root.codex]\nmodel = \"m\"\ngateway = \"g\"\nsandbox = \"workspace-write\"\n"))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want codex named whole to hold", err)
	}
}
