package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// What case a door's key arrives in, asked of viper rather than assumed. The
// namespace it opens onto is matched by exact string, so this decides whether
// a namespace with a capital in it can be reached at all.
func TestWhatCaseADoorKeyArrivesIn(t *testing.T) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader("[auth.door.Clean]\nrp_id = \"cleanamsterdam.example\"\n")); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatal(err)
	}

	for key := range cfg.Auth.Door {
		t.Logf("written as %q, arrives as %q", "Clean", key)
	}
}
