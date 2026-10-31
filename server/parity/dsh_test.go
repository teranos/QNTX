package parity

import (
	"strings"
	"testing"
)

func TestDshIsPinned(t *testing.T) {
	pin, err := PinOf(".", "dsh")
	if err != nil {
		t.Fatalf("PinOf: %v", err)
	}
	version, tarball, err := Dsh()
	if err != nil {
		t.Fatalf("Dsh: %v", err)
	}
	if version != pin.Version {
		t.Errorf("pin is %s, manifest is %s", pin.Version, version)
	}
	if tarball.File != "dsh-"+version+".tgz" {
		t.Errorf("the pinned npm tarball is %q", tarball.File)
	}
	if len(tarball.SHA256) != 64 || strings.Trim(tarball.SHA256, "0123456789abcdef") != "" {
		t.Errorf("the npm tarball has no valid SHA-256")
	}
	if !strings.HasPrefix(tarball.Integrity, "sha512-") {
		t.Errorf("the npm tarball has no npm integrity")
	}
}
