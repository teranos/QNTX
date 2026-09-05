package config

import (
	"strings"
	"testing"
)

// What the developer portal hands an operator: a Services ID, the team, and a
// key by id and by reference to where it is kept.
func TestAppleProviderLoads(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.apple]
client_id   = "nl.sbvh.q.web"
team_id     = "DEF123GHIJ"
key_id      = "ABC123DEFG"
private_key = "ssm:///q/box/apple/private-key"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	apple := cfg.Auth.Provider.Apple
	if apple.ClientID != "nl.sbvh.q.web" || apple.TeamID != "DEF123GHIJ" || apple.KeyID != "ABC123DEFG" {
		t.Errorf("apple = %+v", apple)
	}
	if apple.PrivateKeyRef != "ssm:///q/box/apple/private-key" {
		t.Errorf("PrivateKeyRef = %q", apple.PrivateKeyRef)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// The key is the secret. am.toml ships as a world-readable SSM String
// parameter, so a key written here is already disclosed.
func TestApplePrivateKeyLiteralRejected(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.provider.apple]
client_id   = "nl.sbvh.q.web"
team_id     = "DEF123GHIJ"
key_id      = "ABC123DEFG"
private_key = "-----BEGIN PRIVATE KEY-----\nMIGT\n-----END PRIVATE KEY-----"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a literal private key")
	}
	if !strings.Contains(err.Error(), "auth.provider.apple.private_key") {
		t.Errorf("error does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "MIGT") {
		t.Errorf("error leaked the rejected value: %v", err)
	}
}

// Apple needs all four or it needs nothing. Three is a provider that gets
// drawn and then fails, so it is refused where the operator can still see why.
func TestApplePartOfAClientRejected(t *testing.T) {
	for what, body := range map[string]string{
		"no key": `
[auth.provider.apple]
client_id = "nl.sbvh.q.web"
team_id   = "DEF123GHIJ"
key_id    = "ABC123DEFG"
`,
		"no team": `
[auth.provider.apple]
client_id   = "nl.sbvh.q.web"
key_id      = "ABC123DEFG"
private_key = "ssm:///q/box/apple/private-key"
`,
		"no key id": `
[auth.provider.apple]
client_id   = "nl.sbvh.q.web"
team_id     = "DEF123GHIJ"
private_key = "ssm:///q/box/apple/private-key"
`,
		"no client id": `
[auth.provider.apple]
team_id     = "DEF123GHIJ"
key_id      = "ABC123DEFG"
private_key = "ssm:///q/box/apple/private-key"
`,
		"at a door": `
[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://garden.test"]
[auth.door.garden.provider.apple]
client_id = "garden.services.id"
`,
	} {
		cfg, err := LoadFromFile(writeConfig(t, body))
		if err != nil {
			t.Fatalf("%s: LoadFromFile = %v", what, err)
		}
		err = cfg.Validate()
		if err == nil {
			t.Errorf("%s: Validate accepted it", what)
			continue
		}
		if !strings.Contains(err.Error(), "apple") {
			t.Errorf("%s: error does not name the provider: %v", what, err)
		}
	}
}

// A node offering no Apple is the ordinary case and needs no section.
func TestAppleAbsent(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth]
enabled = true
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Auth.Provider.Apple.ClientID != "" {
		t.Errorf("ClientID = %q, want empty", cfg.Auth.Provider.Apple.ClientID)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
