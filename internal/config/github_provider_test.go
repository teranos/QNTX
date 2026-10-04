package config

import (
	"strings"
	"testing"
)

// The GitHub client an operator registered, read out of where they put it.
func TestGitHubProviderLoads(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.github]
client_id     = "Iv23li-the-operators-app"
client_secret = "ssm:///q/box/github/client-secret"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if got := cfg.Auth.Provider.GitHub.ClientID; got != "Iv23li-the-operators-app" {
		t.Errorf("ClientID = %q", got)
	}
	if got := cfg.Auth.Provider.GitHub.ClientSecretRef; got != "ssm:///q/box/github/client-secret" {
		t.Errorf("ClientSecretRef = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// A literal secret in a world-readable am.toml is already disclosed.
func TestGitHubClientSecretLiteralRejected(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.github]
client_id     = "Iv23li-the-operators-app"
client_secret = "a-literal-secret"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate = nil, want the literal secret refused")
	}
	if !strings.Contains(err.Error(), "auth.provider.github.client_secret") {
		t.Errorf("error does not name the field: %v", err)
	}
}

// The App's private key signs what the App does as itself (ADR-043).
func TestGitHubPrivateKeyLoads(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.github]
client_id     = "Iv23li-the-operators-app"
client_secret = "ssm:///q/box/github/client-secret"
private_key   = "ssm:///q/box/github/private-key"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if got := cfg.Auth.Provider.GitHub.PrivateKeyRef; got != "ssm:///q/box/github/private-key" {
		t.Errorf("PrivateKeyRef = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

func TestGitHubPrivateKeyLiteralRejected(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.github]
client_id     = "Iv23li-the-operators-app"
client_secret = "ssm:///q/box/github/client-secret"
private_key   = "-----BEGIN RSA PRIVATE KEY-----"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate = nil, want the literal key refused")
	}
	if !strings.Contains(err.Error(), "auth.provider.github.private_key") {
		t.Errorf("error does not name the field: %v", err)
	}
}

// The JWT the key signs names the App by its client_id: a key with no client
// signs for nobody.
func TestGitHubPrivateKeyWithoutClientRejected(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.github]
private_key = "ssm:///q/box/github/private-key"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate = nil, want a key without its App's client refused")
	}
	if !strings.Contains(err.Error(), "auth.provider.github") {
		t.Errorf("error does not name the provider: %v", err)
	}
}
