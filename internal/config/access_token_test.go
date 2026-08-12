package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// writeConfig writes a temporary am.toml and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "am.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// Why the host is a value and not a key.
//
// A host used as a key survives a direct Get, but not the Unmarshal that
// building Config goes through: mapstructure splits "github.com" at the dot and
// then finds a nested map where it wants a string. Because Load unmarshals, that
// error fails the whole config load — every other setting included. A host as a
// value has nothing to split.
//
// This pins the behaviour instead of arguing about it. If it ever starts
// passing, the array-of-tables shape may no longer be necessary.
func TestHostAsKeyBreaksConfigLoad(t *testing.T) {
	path := writeConfig(t, `
[plugin.access_token]
"github.com" = "ssm:///qntx/github-token"
"codeberg.org" = "env:CODEBERG_TOKEN"
`)

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig = %v", err)
	}

	// A direct lookup keeps the dotted key intact — this path was never broken.
	direct := v.GetStringMapString("plugin.access_token")
	if direct["github.com"] != "ssm:///qntx/github-token" {
		t.Errorf("GetStringMapString lost the host key: %#v", direct)
	}

	// The path Config actually takes does not survive it.
	var viaStruct struct {
		Plugin struct {
			AccessToken map[string]string `mapstructure:"access_token"`
		} `mapstructure:"plugin"`
	}
	err := v.Unmarshal(&viaStruct)
	if err == nil {
		t.Fatalf("Unmarshal accepted a host-keyed map (%#v) — dots in keys are no longer split",
			viaStruct.Plugin.AccessToken)
	}

	// The error names the split fragment, not the host.
	if !strings.Contains(err.Error(), "access_token[github]") {
		t.Errorf("expected the host to be split at the dot, got: %v", err)
	}
	t.Logf("confirmed, host-keyed map fails config load: %v", err)
}

// A forge host is a value, not a key. The config keyspace splits on dots, and
// every forge host has one, so this is the case that matters.
func TestAccessTokenHostWithDots(t *testing.T) {
	path := writeConfig(t, `
[plugin]
enabled = ["https://github.com/sbvh-nl/duif"]

[[plugin.access_token]]
host = "github.com"
ref  = "ssm:///qntx/github-token"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v, want nil — a host containing a dot must survive config load", err)
	}

	if len(cfg.Plugin.AccessToken) != 1 {
		t.Fatalf("AccessToken has %d entries, want 1: %+v", len(cfg.Plugin.AccessToken), cfg.Plugin.AccessToken)
	}

	entry := cfg.Plugin.AccessToken[0]
	if entry.Host != "github.com" {
		t.Errorf("Host = %q, want %q — the dot must not become nesting", entry.Host, "github.com")
	}
	if entry.Ref != "ssm:///qntx/github-token" {
		t.Errorf("Ref = %q, want the ssm reference", entry.Ref)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// Several forges, one credential each, set centrally rather than per plugin.
func TestAccessTokenMultipleHosts(t *testing.T) {
	path := writeConfig(t, `
[[plugin.access_token]]
host = "github.com"
ref  = "ssm:///qntx/github-token"

[[plugin.access_token]]
host = "codeberg.org"
ref  = "env:CODEBERG_TOKEN"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	if got := cfg.Plugin.RefForHost("github.com"); got != "ssm:///qntx/github-token" {
		t.Errorf("RefForHost(github.com) = %q, want the ssm reference", got)
	}
	if got := cfg.Plugin.RefForHost("codeberg.org"); got != "env:CODEBERG_TOKEN" {
		t.Errorf("RefForHost(codeberg.org) = %q, want the env reference", got)
	}

	// An unconfigured host is not an error — a public repo needs no credential.
	if got := cfg.Plugin.RefForHost("gitlab.com"); got != "" {
		t.Errorf("RefForHost(gitlab.com) = %q, want \"\"", got)
	}

	// Hosts are case-insensitive.
	if got := cfg.Plugin.RefForHost("GitHub.com"); got != "ssm:///qntx/github-token" {
		t.Errorf("RefForHost(GitHub.com) = %q, want the ssm reference", got)
	}
}

// The literal-token rejection must survive the shape change, and must name the
// host so the operator knows which entry to fix.
func TestAccessTokenLiteralRejected(t *testing.T) {
	path := writeConfig(t, `
[[plugin.access_token]]
host = "github.com"
ref  = "ghp_averyrealisticlookingliteral"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a literal token")
	}
	if !strings.Contains(err.Error(), "github.com") {
		t.Errorf("error does not name the host: %v", err)
	}
	// Still must not echo the value it rejected.
	if strings.Contains(err.Error(), "ghp_averyrealisticlookingliteral") {
		t.Errorf("error leaked the rejected value: %v", err)
	}
}

// An entry with a ref but no host is unusable and must not load silently.
func TestAccessTokenMissingHostRejected(t *testing.T) {
	path := writeConfig(t, `
[[plugin.access_token]]
ref = "ssm:///qntx/github-token"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted an access_token entry with no host")
	}
}

// No access_token section at all is valid — public repos need no credential.
func TestAccessTokenAbsent(t *testing.T) {
	path := writeConfig(t, `
[plugin]
enabled = ["meili"]
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if len(cfg.Plugin.AccessToken) != 0 {
		t.Errorf("AccessToken = %+v, want empty", cfg.Plugin.AccessToken)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
	if got := cfg.Plugin.RefForHost("github.com"); got != "" {
		t.Errorf("RefForHost = %q, want \"\"", got)
	}
}
