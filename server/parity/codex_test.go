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
    }
}
