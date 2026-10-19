package claudecode

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A node a system service runs is handed no bus, so a turn apart in its user's systemd is given
// the one its runtime directory holds.
func TestATurnApartIsHandedItsUsersBus(t *testing.T) {
	runtime := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtime, "bus"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	bus, err := userBus("kapteyn", runtime)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"XDG_RUNTIME_DIR=" + runtime, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(runtime, "bus")}
	if !slices.Equal(bus, want) {
		t.Errorf("bus = %v, want %v", bus, want)
	}
}

// A user whose systemd is not running has no bus, and the turn says how to have one.
func TestATurnApartWithNoUserSystemdSaysToLinger(t *testing.T) {
	_, err := userBus("kapteyn", t.TempDir())
	if err == nil {
		t.Fatal("a runtime directory with no bus was handed as one")
	}
	if !strings.Contains(err.Error(), "loginctl enable-linger kapteyn") {
		t.Errorf("the refusal does not say how to have a bus: %v", err)
	}
}
