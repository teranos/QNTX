package server

import "testing"

// The node's process is a service with a PATH of its own, and gh installed on
// the box for a shell is not on it: "exec: \"gh\": executable file not found
// in $PATH", three seconds after the first push the node ever watched. The
// places a nix box puts it are asked in turn, and PATH first.
func TestGhPathFallsBackToWhereANixBoxKeepsIt(t *testing.T) {
	present := map[string]bool{"/run/current-system/sw/bin/gh": true}
	got := ghPathAmong(func(string) (string, error) { return "", errNotOnPath },
		func(p string) bool { return present[p] })
	if got != "/run/current-system/sw/bin/gh" {
		t.Fatalf("resolved %q; wanted the system profile's gh", got)
	}
}

func TestGhPathPrefersPATH(t *testing.T) {
	got := ghPathAmong(func(string) (string, error) { return "/usr/local/bin/gh", nil },
		func(string) bool { return true })
	if got != "/usr/local/bin/gh" {
		t.Fatalf("resolved %q; wanted what PATH said", got)
	}
}

// Nowhere is still "gh": the command runs, fails as it did, and the row says
// so by name rather than by an empty argv.
func TestGhPathIsGhWhenNowhere(t *testing.T) {
	got := ghPathAmong(func(string) (string, error) { return "", errNotOnPath },
		func(string) bool { return false })
	if got != "gh" {
		t.Fatalf("resolved %q; wanted the bare name", got)
	}
}
