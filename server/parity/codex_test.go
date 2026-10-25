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
        // Every platform runs the code-mode host beside codex; Linux runs a
        // command in bubblewrap, macOS in its own sandbox.
        want := map[string]string{"codex-code-mode-host": "codex-code-mode-host-"}
        if strings.HasPrefix(platform, "linux-") {
            want["codex-resources/bwrap"] = "bwrap-"
        }
        if len(artifact.Beside) != len(want) {
            t.Errorf("%s pins %d archives beside codex, want %d", platform, len(artifact.Beside), len(want))
        }
        for _, b := range artifact.Beside {
            prefix, ok := want[b.Path]
            if !ok || !strings.HasPrefix(b.File, prefix) || len(b.SHA256) != 64 {
                t.Errorf("%s pins %s at %s", platform, b.File, b.Path)
            }
        }
    }
}
