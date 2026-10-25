package parity

import (
    "strings"
    "testing"
)

func TestCodexIsPinned(t *testing.T) {
    pin, err := PinOf(".", "codex")
    if err != nil { t.Fatalf("PinOf: %v", err) }
    version, artifacts, err := Codex()
    if err != nil { t.Fatalf("Codex: %v", err) }
    if version != pin.Version { t.Errorf("pin is %s, manifest is %s", pin.Version, version) }
    for _, platform := range []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64"} {
        artifact, ok := artifacts[platform]
        if !ok || !strings.HasPrefix(artifact.File, "codex-") || !strings.HasSuffix(artifact.File, ".tar.gz") {
            t.Errorf("%s has no pinned Codex release archive", platform)
        }
        if len(artifact.SHA256) != 64 || strings.Trim(artifact.SHA256, "0123456789abcdef") != "" {
            t.Errorf("%s has no valid archive SHA-256", platform)
        }
        // Linux runs a command in bubblewrap; macOS in its own sandbox.
        linux := strings.HasPrefix(platform, "linux-")
        if (artifact.Sandbox != nil) != linux {
            t.Errorf("%s pins a sandbox archive: %v", platform, artifact.Sandbox != nil)
        }
        if linux && (artifact.Sandbox == nil || !strings.HasPrefix(artifact.Sandbox.File, "bwrap-") || len(artifact.Sandbox.SHA256) != 64) {
            t.Errorf("%s has no pinned bwrap release archive", platform)
        }
    }
}
