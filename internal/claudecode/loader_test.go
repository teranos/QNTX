package claudecode

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// A stand-in for Claude Code is a script, and a script runs as it is.
func TestAScriptRunsAsItIs(t *testing.T) {
	binary, _ := standIn(t, "", 0, "")
	run, argv, err := Invocation(binary, []string{"auth", "status"})
	if err != nil {
		t.Fatalf("Invocation: %v", err)
	}
	if run != binary || !slices.Equal(argv, []string{"auth", "status"}) {
		t.Errorf("a script ran as %s %v", run, argv)
	}
}

// An ELF whose loader is on this machine runs as it is.
func TestAnELFWhoseLoaderIsHereRunsAsItIs(t *testing.T) {
	self := ownELF(t)
	run, argv, err := Invocation(self, []string{"--version"})
	if err != nil {
		t.Fatalf("Invocation: %v", err)
	}
	if run != self || !slices.Equal(argv, []string{"--version"}) {
		t.Errorf("an ELF whose loader is here ran as %s %v", run, argv)
	}
}

// "NixOS cannot run dynamically linked executables intended for generic linux environments out of the box"
// An ELF naming a loader this machine lacks runs through the node's own, handed the ELF.
func TestAnELFWhoseLoaderIsNotHereRunsThroughTheNodesOwn(t *testing.T) {
	self := ownELF(t)
	own, _, err := interpreterOf(self)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// The same length, so the ELF stays whole: only where its loader is changes.
	missing := "/nowhere" + strings.Repeat("x", len(own)-len("/nowhere"))
	at := bytes.Index(raw, []byte(own+"\x00"))
	if at < 0 {
		t.Fatalf("%s does not hold its own loader %s", self, own)
	}
	copy(raw[at:], missing)
	elsewhere := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(elsewhere, raw, 0o755); err != nil {
		t.Fatal(err)
	}

	run, argv, err := Invocation(elsewhere, []string{"--version"})
	if err != nil {
		t.Fatalf("Invocation: %v", err)
	}
	want := []string{"--library-path", filepath.Dir(own), elsewhere, "--version"}
	if run != own || !slices.Equal(argv, want) {
		t.Errorf("ran as %s %v, not %s %v", run, argv, own, want)
	}
}

// "Could not start dynamically linked executable"
// On k the loader an ELF names is there, NixOS's stub-ld, which only says it cannot run it.
// A file there that names itself no shared object is no loader: the ELF runs through the node's own.
func TestAnELFWhoseLoaderIsAStubRunsThroughTheNodesOwn(t *testing.T) {
	self := ownELF(t)
	own, _, err := interpreterOf(self)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// The stub's path is as long as the loader's, so the ELF stays whole.
	prefix := "/tmp/stub-ld-"
	if len(prefix) >= len(own) {
		t.Skipf("%s is too short a loader path to stand a stub in for", own)
	}
	stub := prefix + strings.Repeat("x", len(own)-len(prefix))
	// An executable, as the stub is: no SONAME names it a shared object.
	if err := os.WriteFile(stub, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(stub); err != nil {
			t.Errorf("the stub %s was not removed: %v", stub, err)
		}
	})
	at := bytes.Index(raw, []byte(own+"\x00"))
	if at < 0 {
		t.Fatalf("%s does not hold its own loader %s", self, own)
	}
	copy(raw[at:], stub)
	claude := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(claude, raw, 0o755); err != nil {
		t.Fatal(err)
	}

	run, argv, err := Invocation(claude, []string{"--version"})
	if err != nil {
		t.Fatalf("Invocation: %v", err)
	}
	want := []string{"--library-path", filepath.Dir(own), claude, "--version"}
	if run != own || !slices.Equal(argv, want) {
		t.Errorf("ran as %s %v, not %s %v", run, argv, own, want)
	}
}

// ownELF is this test's own binary, when it is a Linux ELF that names a loader.
func ownELF(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("a loader is a Linux ELF's")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loader, isELF, err := interpreterOf(self)
	if err != nil || !isELF {
		t.Skipf("this test binary names no loader to stand in with: %v", err)
	}
	if _, err := os.Stat(loader); err != nil {
		t.Skipf("this test binary's own loader %s is not here: %v", loader, err)
	}
	return self
}
