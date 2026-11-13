package config

import (
	"strings"
	"testing"
)

// 「[Fable 5.2] What the developer portal hands an operator for push: the app, the team,」
// 「[Fable 5.2] and a key by its id and by reference to where it is kept.」
func TestApnsLoads(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[apns]
bundle_id   = "com.example.qntx"
team_id     = "DEF123GHIJ"
key_id      = "ABC123DEFG"
private_key = "ssm:///q/box/apns/private-key"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if cfg.Apns.BundleID != "com.example.qntx" || cfg.Apns.KeyID != "ABC123DEFG" || cfg.Apns.TeamID != "DEF123GHIJ" || cfg.Apns.PrivateKeyRef != "ssm:///q/box/apns/private-key" {
		t.Errorf("apns was read as %+v", cfg.Apns)
	}
	if cfg.Apns.Development {
		t.Error("an app is reached at Apple's production host unless am.toml says development")
	}
}

// 「[Fable 5.2] The key is the secret, and am.toml is read by more than the node: a key」
// 「[Fable 5.2] written here is already disclosed.」
func TestApnsPrivateKeyLiteralRejected(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[apns]
bundle_id   = "com.example.qntx"
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
	if !strings.Contains(err.Error(), "apns.private_key") {
		t.Errorf("error does not name the field: %v", err)
	}
	if strings.Contains(err.Error(), "MIGT") {
		t.Errorf("error leaked the rejected value: %v", err)
	}
}

// 「[Fable 5.2] A key with no app to reach, or an app with no key, is said when the node starts.」
func TestApnsIsAllFourOrNone(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[apns]
bundle_id = "com.example.qntx"
key_id    = "ABC123DEFG"
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "together, or none") {
		t.Fatalf("Validate = %v, and half an [apns] is no way to a phone", err)
	}

	none, err := LoadFromFile(writeConfig(t, ``))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := none.Validate(); err != nil {
		t.Fatalf("a node that reaches no phone is refused: %v", err)
	}
}
