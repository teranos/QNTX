package parity

import "testing"

func TestHermesIsPinned(t *testing.T) {
	pin, err := PinOf(".", "hermes")
	if err != nil {
		t.Fatalf("PinOf: %v", err)
	}
	version, commit, err := Hermes()
	if err != nil {
		t.Fatalf("Hermes: %v", err)
	}
	if version != pin.Version {
		t.Errorf("manifest is for %s, directory names %s", version, pin.Version)
	}
	if commit[:7] != pin.Rev {
		t.Errorf("manifest commit %s differs from directory revision %s", commit, pin.Rev)
	}
}
