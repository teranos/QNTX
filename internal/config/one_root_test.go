package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// "how many instances of the same thing are there in it that can easily be
// derived and overwritten on divergence ?"

// loadAm reads an am.toml the way the node does, from defaults up.
func loadAm(t *testing.T, amToml string) (*Config, *viper.Viper) {
	t.Helper()
	v := viper.New()
	SetDefaults(v)
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader(amToml)); err != nil {
		t.Fatalf("am.toml did not parse: %v\n%s", err, amToml)
	}
	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper failed: %v", err)
	}
	return cfg, v
}

func countOf(origins []string, origin string) int {
	n := 0
	for _, o := range origins {
		if o == origin {
			n++
		}
	}
	return n
}

// --- Tim: happy path ---

// Tim writes rp_id once and a door with one origin, and nothing else.
func TestTim_OneRootIsWrittenOnce(t *testing.T) {
	cfg, _ := loadAm(t, `
[auth]
rp_id = "garden.test"

[auth.door.pond]
origins = ["https://pond.test"]
`)

	if !slices.Equal(cfg.Auth.RPOrigins, []string{"https://garden.test"}) {
		t.Errorf("rp_origins = %v, want [https://garden.test]", cfg.Auth.RPOrigins)
	}
	if cfg.Mail.From != "system@garden.test" {
		t.Errorf("mail.from = %q, want system@garden.test", cfg.Mail.From)
	}
	if got := cfg.Auth.Door["pond"].RPID; got != "pond.test" {
		t.Errorf("auth.door.pond.rp_id = %q, want pond.test", got)
	}
	allowed := cfg.GetServerAllowedOrigins()
	for _, want := range []string{"https://garden.test", "https://pond.test"} {
		if countOf(allowed, want) != 1 {
			t.Errorf("allowed origins hold %q %d times, want once: %v", want, countOf(allowed, want), allowed)
		}
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate refused what was derived: %v", err)
	}
}

// Tim's written values stand where they diverge from what rp_id says.
func TestTim_WrittenOverridesDerived(t *testing.T) {
	cfg, _ := loadAm(t, `
[mail]
from = "Garden <mail@garden.test>"

[auth]
rp_id = "garden.test"
rp_origins = ["https://garden.test", "qntx://door"]

[auth.door.pond]
rp_id = "pond.test"
origins = ["https://portal.pond.test"]
`)

	if !slices.Equal(cfg.Auth.RPOrigins, []string{"https://garden.test", "qntx://door"}) {
		t.Errorf("rp_origins = %v, want what was written", cfg.Auth.RPOrigins)
	}
	if cfg.Mail.From != "Garden <mail@garden.test>" {
		t.Errorf("mail.from = %q, want what was written", cfg.Mail.From)
	}
	if got := cfg.Auth.Door["pond"].RPID; got != "pond.test" {
		t.Errorf("auth.door.pond.rp_id = %q, want what was written", got)
	}
}

// --- Spike: edge cases ---

// No rp_id is no root: nothing is derived from it.
func TestSpike_NoRootDerivesNothing(t *testing.T) {
	cfg, _ := loadAm(t, `
[auth]
enabled = true
`)
	if len(cfg.Auth.RPOrigins) != 0 {
		t.Errorf("rp_origins = %v, want none: the loopback fallback is auth.New's", cfg.Auth.RPOrigins)
	}
	if cfg.Mail.From != "" {
		t.Errorf("mail.from = %q, want empty", cfg.Mail.From)
	}
}

// rp_id = "localhost" is the dev node, whose page is on a port: rp_origins is
// left to the loopback fallback, and there is no mail domain.
func TestSpike_LocalhostIsNotARoot(t *testing.T) {
	cfg, _ := loadAm(t, `
[auth]
rp_id = "localhost"
`)
	if len(cfg.Auth.RPOrigins) != 0 {
		t.Errorf("rp_origins = %v, want none", cfg.Auth.RPOrigins)
	}
	if cfg.Mail.From != "" {
		t.Errorf("mail.from = %q, want empty", cfg.Mail.From)
	}
}

// Zero means zero: written empty is not omitted.
func TestSpike_WrittenEmptyIsNotOmitted(t *testing.T) {
	cfg, _ := loadAm(t, `
[mail]
from = ""

[auth]
rp_id = "garden.test"
rp_origins = []
`)
	if cfg.Mail.From != "" {
		t.Errorf("mail.from = %q, want the empty it was written as", cfg.Mail.From)
	}
	if len(cfg.Auth.RPOrigins) != 0 {
		t.Errorf("rp_origins = %v, want the empty it was written as", cfg.Auth.RPOrigins)
	}
}

// A door's rp_id is derived only where one origin leaves no choice.
func TestSpike_DoorRPIDNeedsExactlyOneWebOrigin(t *testing.T) {
	cases := []struct {
		name    string
		origins string
		want    string
	}{
		{"two origins", `["https://a.pond.test", "https://b.pond.test"]`, ""},
		{"web origin and an app scheme", `["https://pond.test", "pond://door"]`, ""},
		{"an app scheme alone", `["pond://door"]`, ""},
		{"a port is not part of the host", `["https://pond.test:8443"]`, "pond.test"},
		{"a trailing slash is not part of the host", `["https://pond.test/"]`, "pond.test"},
		{"upper case is the same host", `["https://Pond.Test"]`, "pond.test"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, _ := loadAm(t, "[auth.door.pond]\norigins = "+c.origins+"\n")
			if got := cfg.Auth.Door["pond"].RPID; got != c.want {
				t.Errorf("auth.door.pond.rp_id = %q, want %q", got, c.want)
			}
		})
	}
}

// A derived door rp_id that is left empty is still refused at load.
func TestSpike_UnderivableDoorIsStillRefused(t *testing.T) {
	cfg, _ := loadAm(t, `
[auth.door.pond]
origins = ["https://a.pond.test", "https://b.pond.test"]
`)
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "rp_id") {
		t.Errorf("Validate said %v, want a refusal naming rp_id", err)
	}
}

// An app's scheme is a door, never a page a browser sends Origin from: it is
// not added to CORS. Written, it still counts.
func TestSpike_AppSchemesAreNotAddedToAllowedOrigins(t *testing.T) {
	cfg, _ := loadAm(t, `
[auth]
rp_id = "garden.test"
rp_origins = ["https://garden.test", "qntx://door"]

[auth.door.pond]
rp_id = "pond.test"
origins = ["https://pond.test", "pond://door"]
`)
	allowed := cfg.GetServerAllowedOrigins()
	for _, scheme := range []string{"qntx://door", "pond://door"} {
		if slices.Contains(allowed, scheme) {
			t.Errorf("allowed origins hold %q: %v", scheme, allowed)
		}
	}
	if !slices.Contains(allowed, "https://pond.test") {
		t.Errorf("allowed origins lack the door's web origin: %v", allowed)
	}
}

// --- Jenny: complex scenarios ---

// Jenny's am.toml is written out the way a deployment's is today: every door
// origin in allowed_origins as well. Nothing written is dropped, nothing is
// held twice.
func TestJenny_ADeploymentWrittenOutStaysWhole(t *testing.T) {
	cfg, _ := loadAm(t, `
[server]
allowed_origins = [
  "https://garden.test",
  "https://portal.pond.test",
  "https://shop.pond.test",
  "https://field.test",
  "https://portal.field.test",
  "http://localhost",
]

[auth]
rp_id = "garden.test"
rp_origins = ["https://garden.test", "qntx://door"]

[auth.door.pond]
rp_id   = "pond.test"
origins = ["https://portal.pond.test"]

[auth.door.shop]
rp_id   = "shop.pond.test"
origins = ["https://shop.pond.test"]

[auth.door.field]
rp_id   = "field.test"
origins = ["https://field.test"]
`)
	allowed := cfg.GetServerAllowedOrigins()
	for _, want := range []string{
		"https://garden.test",
		"https://portal.pond.test",
		"https://shop.pond.test",
		"https://field.test",
		"https://portal.field.test",
		"http://localhost",
	} {
		if countOf(allowed, want) != 1 {
			t.Errorf("allowed origins hold %q %d times, want once: %v", want, countOf(allowed, want), allowed)
		}
	}
	if cfg.Mail.From != "system@garden.test" {
		t.Errorf("mail.from = %q, want system@garden.test", cfg.Mail.From)
	}
	if got := cfg.Auth.Door["pond"].RPID; got != "pond.test" {
		t.Errorf("a written door rp_id that diverges from its origin's host was replaced: %q", got)
	}
}

// The same am.toml once nothing derivable is written: the node reaches the
// same origins, relying parties and sender.
func TestJenny_TheSameDeploymentWithOneRoot(t *testing.T) {
	cfg, _ := loadAm(t, `
[server]
allowed_origins = [
  "https://portal.field.test",
  "http://localhost",
]

[auth]
rp_id = "garden.test"
rp_origins = ["https://garden.test", "qntx://door"]

[auth.door.pond]
rp_id   = "pond.test"
origins = ["https://portal.pond.test"]

[auth.door.shop]
origins = ["https://shop.pond.test"]

[auth.door.field]
origins = ["https://field.test"]
`)
	allowed := cfg.GetServerAllowedOrigins()
	for _, want := range []string{
		"https://garden.test",
		"https://portal.pond.test",
		"https://shop.pond.test",
		"https://field.test",
		"https://portal.field.test",
	} {
		if countOf(allowed, want) != 1 {
			t.Errorf("allowed origins hold %q %d times, want once: %v", want, countOf(allowed, want), allowed)
		}
	}
	if got := cfg.Auth.Door["shop"].RPID; got != "shop.pond.test" {
		t.Errorf("auth.door.shop.rp_id = %q, want shop.pond.test", got)
	}
	if got := cfg.Auth.Door["field"].RPID; got != "field.test" {
		t.Errorf("auth.door.field.rp_id = %q, want field.test", got)
	}
	if cfg.Mail.From != "system@garden.test" {
		t.Errorf("mail.from = %q, want system@garden.test", cfg.Mail.From)
	}
}

// A reload derives from a fresh read; deriving twice over one Config changes
// nothing either.
func TestJenny_DerivingTwiceIsDerivingOnce(t *testing.T) {
	cfg, v := loadAm(t, `
[server]
allowed_origins = ["https://garden.test"]

[auth]
rp_id = "garden.test"

[auth.door.pond]
origins = ["https://pond.test"]
`)
	once := slices.Clone(cfg.Server.AllowedOrigins)
	cfg.fromOneRoot(v)
	if !slices.Equal(cfg.Server.AllowedOrigins, once) {
		t.Errorf("a second derivation moved allowed_origins from %v to %v", once, cfg.Server.AllowedOrigins)
	}
	if !slices.Equal(cfg.Auth.RPOrigins, []string{"https://garden.test"}) {
		t.Errorf("rp_origins = %v after a second derivation", cfg.Auth.RPOrigins)
	}
}
