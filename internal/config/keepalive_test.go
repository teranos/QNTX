package config

import (
	"strings"
	"testing"
)

// A node whose config does not enable keepalive runs none: an omitted table is
// not a keepalive at intervals nobody wrote down.
func TestKeepaliveLeftOutRunsNone(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[plugin]
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if cfg.Plugin.WebSocket.Keepalive.Enabled {
		t.Errorf("keepalive is enabled by a config that does not say so: %+v", cfg.Plugin.WebSocket.Keepalive)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

// An enabled keepalive names how often it pings and how long it waits for a
// PONG, or the config is refused naming the key it left out.
func TestAnEnabledKeepaliveNamingNoIntervalIsRefused(t *testing.T) {
	cfg, err := LoadFromFile(writeConfig(t, `
[plugin.websocket.keepalive]
enabled = true
pong_timeout_secs = 60
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ping_interval_secs") {
		t.Errorf("Validate = %v, want it to name ping_interval_secs", err)
	}

	cfg, err = LoadFromFile(writeConfig(t, `
[plugin.websocket.keepalive]
enabled = true
ping_interval_secs = 30
pong_timeout_secs = 60
`))
	if err != nil {
		t.Fatalf("LoadFromFile = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
