package config

import (
	"strings"
	"testing"
)

// The app an operator registered at Meta, read out of the place they put it.
func TestMetaProviderLoads(t *testing.T) {
	path := writeConfig(t, `
[auth.provider.meta]
client_id     = "the-operators-app"
client_secret = "ssm:///q/box/meta/client-secret"
`)

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if got := cfg.Auth.Provider.Meta.ClientID; got != "the-operators-app" {
		t.Errorf("ClientID = %q", got)
	}
	if got := cfg.Auth.Provider.Meta.ClientSecretRef; got != "ssm:///q/box/meta/client-secret" {
		t.Errorf("ClientSecretRef = %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// Same rule as Google's, for the same reason: am.toml ships world-readable.
func TestMetaClientSecretLiteralRejected(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.provider.meta]
client_id     = "the-operators-app"
client_secret = "a-literal-where-a-reference-belongs"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a literal client secret")
	}
	if !strings.Contains(err.Error(), "auth.provider.meta.client_secret") {
		t.Errorf("error does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "a-literal-where-a-reference-belongs") {
		t.Errorf("error leaked the rejected value: %v", err)
	}
}

// Half an app is a button that gets drawn and then fails at the exchange.
func TestMetaHalfAClientRejected(t *testing.T) {
	for what, body := range map[string]string{
		"no secret": `
[auth.provider.meta]
client_id = "1234567890"
`,
		"no client id": `
[auth.provider.meta]
client_secret = "ssm:///q/box/meta/client-secret"
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

// A door may register its own OAuth clients, so somebody arriving there sees a
// consent screen named after the thing they came to.
func TestADoorNamesItsOwnClients(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.provider.google]
client_id     = "the-nodes-client"
client_secret = "ssm:///q/box/google/client-secret"

[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://portal.garden.test"]

[auth.door.garden.provider.google]
client_id     = "gardens-own-client"
client_secret = "ssm:///q/garden/google/client-secret"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
	if got := cfg.Auth.Door["garden"].Provider.Google.ClientID; got != "gardens-own-client" {
		t.Errorf("the door's ClientID = %q", got)
	}
	if got := cfg.Auth.Provider.Google.ClientID; got != "the-nodes-client" {
		t.Errorf("the node's ClientID = %q, want it untouched", got)
	}
}

// A door's client is read the same way the node's is, and a literal is already
// disclosed wherever it is written.
func TestADoorsClientSecretLiteralRejected(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://portal.garden.test"]

[auth.door.garden.provider.meta]
client_id     = "gardens-app"
client_secret = "a-literal-where-a-reference-belongs"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a literal client secret on a door")
	}
	if !strings.Contains(err.Error(), "auth.door.garden.provider.meta.client_secret") {
		t.Errorf("error does not name the door and the field: %v", err)
	}
}

// A door naming no client of its own is the ordinary case.
func TestADoorNeedNotNameAClient(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.door.garden]
rp_id   = "garden.test"
origins = ["https://portal.garden.test"]
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
	if got := cfg.Auth.Door["garden"].Provider.Google.ClientID; got != "" {
		t.Errorf("ClientID = %q, want empty", got)
	}
}

// One provider configured and the other absent is the ordinary case, and
// neither section is a reason to demand the other.
func TestOneProviderConfiguredDoesNotDemandTheOther(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[auth.provider.meta]
client_id     = "the-operators-app"
client_secret = "ssm:///q/box/meta/client-secret"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Auth.Provider.Google.ClientID != "" {
		t.Errorf("Google ClientID = %q, want empty", cfg.Auth.Provider.Google.ClientID)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
