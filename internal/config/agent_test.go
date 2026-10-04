package config

import (
	"strings"
	"testing"
)

// The ROOT agent as am.toml names it (ADR-048): which model, at what effort,
// and where the Claude plan token is kept.
func TestRootAgentLoads(t *testing.T) {
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
		t.Errorf("the ROOT agent loaded as %+v", root)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// A node that names no ROOT agent has none, and that is valid.
func TestNoRootAgentIsValid(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, ``))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Agent.Root.Named() {
		t.Errorf("a node that names no ROOT agent has one: %+v", cfg.Agent.Root)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// A literal token in a world-readable am.toml is already disclosed.
func TestRootAgentTokenLiteralRejected(t *testing.T) {
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
func TestRootAgentNamedByHalfIsRefused(t *testing.T) {
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
