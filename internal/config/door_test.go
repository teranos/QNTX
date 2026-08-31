package config

import (
	"strings"
	"testing"
)

// withDoor builds the least config that passes everything except the doors, so
// a failure below is about the door and nothing else.
func withDoor(doors map[string]DoorConfig) *Config {
	cfg := &Config{}
	cfg.Storage.Backend = "sqlite"
	cfg.Storage.Sqlite.BoundedStorage.ActorContextLimit = 32
	cfg.Storage.Sqlite.BoundedStorage.ActorContextsLimit = 64
	cfg.Storage.Sqlite.BoundedStorage.EntityActorsLimit = 64
	cfg.Auth.Door = doors
	return cfg
}

func TestADoorIsAcceptedWholeOrNotAtAll(t *testing.T) {
	cases := []struct {
		name  string
		door  DoorConfig
		wants string
	}{
		{
			name:  "no rp id",
			door:  DoorConfig{Origins: []string{"https://portal.garden.test"}},
			wants: "rp_id",
		},
		{
			name:  "no origins",
			door:  DoorConfig{RPID: "garden.test"},
			wants: "origins",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := withDoor(map[string]DoorConfig{"garden": c.door}).Validate()
			if err == nil {
				t.Fatalf("Validate accepted a door with %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Errorf("Validate said %q, which does not name %q", err, c.wants)
			}
			if !strings.Contains(err.Error(), "garden") {
				t.Errorf("Validate said %q without naming which door", err)
			}
		})
	}
}

// A door with everything it needs loads.
func TestAWholeDoorLoads(t *testing.T) {
	err := withDoor(map[string]DoorConfig{
		"garden": {
			RPID:    "garden.test",
			Origins: []string{"https://portal.garden.test", "https://app.garden.test"},
		},
	}).Validate()
	if err != nil {
		t.Fatalf("Validate refused a whole door: %v", err)
	}
}

// No doors at all is every deployment today.
func TestNoDoorsIsFine(t *testing.T) {
	if err := withDoor(nil).Validate(); err != nil {
		t.Fatalf("Validate refused a node with no doors: %v", err)
	}
}
