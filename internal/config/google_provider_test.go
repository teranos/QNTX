package config

import (
	"strings"
	"testing"
)

// The OAuth client an operator registered, read out of the place they put it.
func TestGoogleProviderLoads(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.google]
client_id     = "the-operators-client"
client_secret = "ssm:///q/box/google/client-secret"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if got := cfg.Auth.Provider.Google.ClientID; got != "the-operators-client" {
		t.Errorf("ClientID = %q", got)
	}
	if got := cfg.Auth.Provider.Google.ClientSecretRef; got != "ssm:///q/box/google/client-secret" {
		t.Errorf("ClientSecretRef = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// am.toml ships as a world-readable SSM String parameter, so a secret written
// here is already disclosed. Same rule as plugin.access_token.
func TestGoogleClientSecretLiteralRejected(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.google]
client_id     = "the-operators-client"
client_secret = "a-literal-where-a-reference-belongs"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a literal client secret")
	}
	if !strings.Contains(err.Error(), "auth.provider.google.client_secret") {
		t.Errorf("error does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "a-literal-where-a-reference-belongs") {
		t.Errorf("error leaked the rejected value: %v", err)
	}
}

// Half a client is a provider that gets drawn and then fails, so it is refused
// where the operator can still see why.
func TestGoogleHalfAClientRejected(t *testing.T) {
	for what, body := range map[string]string{
		"no secret": `
[auth.provider.google]
client_id = "1234-abc.apps.googleusercontent.com"
`,
		"no client id": `
[auth.provider.google]
client_secret = "ssm:///q/box/google/client-secret"
`,
	} {
		cfg, err := LoadFromFile(writeConfig(t, body))
		if err != nil {
			t.Fatalf("%s: LoadFromFile = %v", what, err)
		}
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", what)
		}
	}
}

// A node offering no Google is the ordinary case and needs no section.
func TestGoogleAbsent(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth]
enabled = true
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Auth.Provider.Google.ClientID != "" {
		t.Errorf("ClientID = %q, want empty", cfg.Auth.Provider.Google.ClientID)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
