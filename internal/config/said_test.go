package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// "absence is absence": a key said as zero is said, and a key left out is not.
func TestAKeySaidAsZeroIsToldApartFromOneLeftOut(t *testing.T) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader("[pulse]\nworkers = 0\n")); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Said("pulse.workers") {
		t.Error("pulse.workers was said as 0 and reads as left out")
	}
	if cfg.Said("pulse.ticker_interval_seconds") {
		t.Error("pulse.ticker_interval_seconds was left out and reads as said")
	}
	if got := cfg.Unsaid("pulse.workers", "pulse.ticker_interval_seconds"); !slices.Equal(got, []string{"pulse.ticker_interval_seconds"}) {
		t.Errorf("unsaid is %v", got)
	}
}

// A squashed table's keys are its parent's, and a map is one key.
func TestTheKeysAreTheTablesKeys(t *testing.T) {
	keys := keysOf(reflect.TypeOf(Config{}), "")
	for _, want := range []string{"server.rate_limit.auth_rate", "auth.provider.google.client_id", "auth.door", "server.port"} {
		if !slices.Contains(keys, want) {
			t.Errorf("%s is not among the keys", want)
		}
	}
	if slices.Contains(keys, "said") {
		t.Error("the record of what was said is read as a key")
	}
}
