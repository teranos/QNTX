// 「[Opus 5.5] Package agy makes "a start on agy as another harness" beside Claude Code, Pi and Codex.」
package agy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teranos/QNTX/internal/pinned"
	"github.com/teranos/QNTX/server/parity"
	errors "github.com/teranos/sacred-error"
)

type Artifact struct{ File, SHA256 string }
type Pin struct {
	Base, Version string
	Artifacts     map[string]Artifact
}

// 「[Opus 5.5] member is what every agy archive holds: agy's installer puts it at ~/.local/bin/agy.」
const member = "antigravity"

// 「[Opus 5.5] updates keeps a pinned agy the pinned agy. Unset, its first run replaces its」
// 「[Opus 5.5] own executable with the newest release (1.4.4 became 1.4.12). Only the value」
// 「[Opus 5.5] "true" holds it: "1", "TRUE" and "yes" each let it update.」
const updates = "AGY_CLI_DISABLE_AUTO_UPDATE=true"

// 「[Opus 5.5] PinnedVersion is the agy Parity pins, as what is said of a turn names it.」
var PinnedVersion = func() string {
	version, _, err := parity.Agy()
	if err != nil {
		return ""
	}
	return version
}()

func Pinned() (Pin, error) {
	version, artifacts, err := parity.Agy()
	if err != nil {
		return Pin{}, err
	}
	p := Pin{Base: "https://github.com/google-antigravity/antigravity-cli/releases/download", Version: version, Artifacts: map[string]Artifact{}}
	for platform, a := range artifacts {
		p.Artifacts[platform] = Artifact(a)
	}
	return p, nil
}

func Platform() (string, error) {
	arch, known := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if !known || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return "", errors.Newf("agy is not pinned for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return runtime.GOOS + "-" + arch, nil
}

// 「[Opus 5.5] Ensure verifies the archive on every call and extracts a fresh executable.」
// 「[Opus 5.5] The pin authenticates the archive; a cached executable has no pinned hash.」
func (p Pin) Ensure(ctx context.Context, dir string) (string, error) {
	platform, err := Platform()
	if err != nil {
		return "", err
	}
	a, ok := p.Artifacts[platform]
	if !ok {
		return "", errors.Newf("agy %s has no archive for %s", p.Version, platform)
	}
	if filepath.Base(p.Version) != p.Version || p.Version == "." || p.Version == ".." || p.Version == "" || filepath.Base(a.File) != a.File || !strings.HasPrefix(a.File, "agy_cli_") || !strings.HasSuffix(a.File, ".tar.gz") {
		return "", errors.Newf("invalid agy version %q or archive %q", p.Version, a.File)
	}
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return "", errors.Newf("agy %s archive %s has invalid sha256 %q", p.Version, a.File, a.SHA256)
	}
	dest := filepath.Join(dir, p.Version)
	if err := os.MkdirAll(dest, 0700); err != nil {
		return "", errors.Wrapf(err, "could not create %s", dest)
	}
	cached := filepath.Join(dest, a.File)
	f, err := pinned.Open(ctx, "agy", p.Base+"/"+p.Version+"/"+a.File, cached, a.SHA256)
	if err != nil {
		return "", err
	}
	defer f.Close()
	target := filepath.Join(dest, "agy")
	if err := pinned.Extract(f, "agy", member, target); err != nil {
		return "", errors.Wrapf(err, "could not install agy %s from %s", p.Version, cached)
	}
	return target, nil
}
