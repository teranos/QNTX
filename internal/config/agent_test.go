package config

import (
	"strings"
	"testing"
)

// The Node Agent as am.toml names it (ADR-048): which model, at what effort,
// and where the Claude plan token is kept.
func TestNodeAgentLoads(t *testing.T) {
	path := writeConfig(t, `
[agent.root]
model  = "claude-opus-5-5"
effort = "low"
token  = "ssm:///q/box/claude/oauth-token"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	root := cfg.Agent.Root
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
[agent.root]
model           = "claude-opus-5-5"
effort          = "low"
permission_mode = "dontAsk"
allow           = ["mcp__qntx", "Bash", "Read"]
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	root := cfg.Agent.Root
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
[agent.root]
model           = "claude-opus-5-5"
effort          = "low"
permission_mode = "everything"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "agent.root.permission_mode") || !strings.Contains(err.Error(), "dontAsk") {
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
[agent.root]
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
	if !strings.Contains(err.Error(), "agent.root.token") {
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
		"agent.root.model":  "[agent.root]\neffort = \"low\"\n",
		"agent.root.effort": "[agent.root]\nmodel = \"claude-opus-5-5\"\n",
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
[agent.root]
model  = "claude-opus-5-5"
effort = "low"

[agent.root.pi]
model    = "anthropic/claude-sonnet-4.6"
thinking = "low"
gateway  = "openrouter-qntx"
`)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	pi := cfg.Agent.Root.Pi
	if !pi.Named() || pi.Model != "anthropic/claude-sonnet-4.6" || pi.Thinking != "low" || pi.Gateway != "openrouter-qntx" {
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
		"agent.root.pi.thinking": "[agent.root.pi]\nmodel = \"m\"\ngateway = \"g\"\nthinking = \"loud\"\n",
	} {
		cfg, err := LoadFromFile(writeConfig(t, "[agent.root]\nmodel = \"claude-opus-5-5\"\neffort = \"low\"\n"+body))
		if err != nil {
			t.Fatalf("LoadFromFile = %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate = %v, want it to say %s", err, want)
		}
	}
}
