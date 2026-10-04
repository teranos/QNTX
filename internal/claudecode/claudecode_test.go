package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// served is a release host holding one binary, counting how often it is asked.
func served(t *testing.T, binary []byte) (*httptest.Server, *int) {
	t.Helper()
	asked := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		platform, err := Platform()
		if err != nil {
			t.Errorf("no platform: %v", err)
		}
		if r.URL.Path != "/9.9.9/"+platform+"/claude" {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(binary); err != nil {
			t.Errorf("the binary was not served: %v", err)
		}
	}))
	t.Cleanup(host.Close)
	return host, &asked
}

// pinning is a pin on binary for the platform the test runs on.
func pinning(t *testing.T, base string, binary []byte) Pin {
	t.Helper()
	platform, err := Platform()
	if err != nil {
		t.Fatalf("no platform: %v", err)
	}
	sum := sha256.Sum256(binary)
	return Pin{Base: base, Version: "9.9.9", Checksums: map[string]string{platform: hex.EncodeToString(sum[:])}}
}

func TestEnsureFetchesWhatThePinNames(t *testing.T) {
	binary := []byte("#!/bin/sh\necho claude\n")
	host, _ := served(t, binary)
	dir := t.TempDir()

	path, err := pinning(t, host.URL, binary).Ensure(context.Background(), dir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if want := filepath.Join(dir, "9.9.9", "claude"); path != want {
		t.Errorf("installed at %s, want %s", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the installed binary did not read: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("the installed binary is not what was served")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("the installed binary is not executable: %v", info.Mode())
	}
}

func TestEnsureRefusesWhatThePinDoesNotName(t *testing.T) {
	host, _ := served(t, []byte("something else"))
	dir := t.TempDir()

	pin := pinning(t, host.URL, []byte("what the pin names"))
	path, err := pin.Ensure(context.Background(), dir)
	if err == nil {
		t.Fatalf("Ensure installed %s, though its hash is not the pin's", path)
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Errorf("the refusal does not say the hash differed: %v", err)
	}
	left, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("readdir: %v", readErr)
	}
	if len(left) != 0 {
		t.Errorf("a refused download left %d entries behind, the first %s", len(left), left[0].Name())
	}
}

func TestEnsureKeepsWhatItHas(t *testing.T) {
	binary := []byte("the binary")
	host, asked := served(t, binary)
	dir := t.TempDir()
	pin := pinning(t, host.URL, binary)

	if _, err := pin.Ensure(context.Background(), dir); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := pin.Ensure(context.Background(), dir); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if *asked != 1 {
		t.Errorf("the host was asked %d times, want 1", *asked)
	}
}

func TestEnsureFetchesAgainWhatWasChanged(t *testing.T) {
	binary := []byte("the binary")
	host, asked := served(t, binary)
	dir := t.TempDir()
	pin := pinning(t, host.URL, binary)

	path, err := pin.Ensure(context.Background(), dir)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed on disk"), 0o755); err != nil {
		t.Fatalf("could not change the binary: %v", err)
	}
	if _, err := pin.Ensure(context.Background(), dir); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(binary) {
		t.Errorf("a binary changed on disk was kept")
	}
	if *asked != 2 {
		t.Errorf("the host was asked %d times, want 2", *asked)
	}
}

func TestEnsureRefusesAPlatformThePinDoesNotCover(t *testing.T) {
	pin := Pin{Base: "http://127.0.0.1:1", Version: "9.9.9", Checksums: map[string]string{}}
	_, err := pin.Ensure(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("Ensure went ahead with no checksum for this platform")
	}
	platform, platformErr := Platform()
	if platformErr != nil {
		t.Fatalf("no platform: %v", platformErr)
	}
	if !strings.Contains(err.Error(), platform) {
		t.Errorf("the refusal does not name %s: %v", platform, err)
	}
}

// The pin itself, held to Anthropic's release host. It downloads the binary,
// so it runs only when asked for by name.
func TestThePinnedClaudeCodeIsWhatAnthropicServes(t *testing.T) {
	if os.Getenv("QNTX_FETCH_CLAUDE_CODE") == "" {
		t.Skip("set QNTX_FETCH_CLAUDE_CODE=1 to download the pinned Claude Code")
	}
	pin, err := Pinned()
	if err != nil {
		t.Fatalf("Pinned: %v", err)
	}
	path, err := pin.Ensure(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	said, err := exec.Command(path, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version: %v", path, err)
	}
	if !strings.HasPrefix(string(said), pin.Version) {
		t.Errorf("the pinned binary says it is %q, and the pin is %s", strings.TrimSpace(string(said)), pin.Version)
	}
}
