package parity

import (
	"strings"
	"testing"
)

// TestClaudeCodeIsPinned: the pin's directory and Anthropic's manifest inside
// it name the same version, and the manifest gives a sha256 per platform.
func TestClaudeCodeIsPinned(t *testing.T) {
	named, err := Pinned(".", "claudecode")
	if err != nil {
		t.Fatalf("Pinned: %v", err)
	}
	version, checksums, err := ClaudeCode()
	if err != nil {
		t.Fatalf("ClaudeCode: %v", err)
	}
	if version != named {
		t.Errorf("the manifest is for %s, and its directory is named for %s", version, named)
	}
	for _, platform := range []string{"linux-x64", "linux-arm64", "darwin-arm64", "darwin-x64"} {
		sum := checksums[platform]
		if len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
			t.Errorf("%s has no sha256 in the manifest: %q", platform, sum)
		}
	}
}
