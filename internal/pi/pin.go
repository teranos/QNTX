package pi

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/teranos/errors"
)

// PinnedFlake is the Pi this build runs: v1.0.3 of earendil-works/pi, by its
// commit, as its own flake builds it.
const PinnedFlake = "github:earendil-works/pi/d78dc83d633229d12f8b79631384c4c2717c399f#pi"

// PinnedVersion is the release that commit is.
const PinnedVersion = "1.0.3"

// nixProfiled is where a multi-user Nix install keeps nix, which a service's
// PATH does not carry.
const nixProfiled = "/nix/var/nix/profiles/default/bin/nix"

// Nix is the nix this machine has: on PATH, or where a Nix install puts it.
func Nix() (string, error) {
	if found, err := exec.LookPath("nix"); err == nil {
		return found, nil
	}
	if _, err := os.Stat(nixProfiled); err == nil {
		return nixProfiled, nil
	}
	return "", errors.Newf("this machine has no nix, on PATH or at %s, to build Pi with", nixProfiled)
}

// Ensure builds the pinned Pi with nix and answers where its binary is. A
// build Nix already holds answers at once.
func Ensure(ctx context.Context, nix, flake string) (string, error) {
	cmd := exec.CommandContext(ctx, nix, "--extra-experimental-features", "nix-command flakes",
		"build", "--no-link", "--print-out-paths", flake)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		if len(said) > stderrKept {
			said = said[len(said)-stderrKept:]
		}
		return "", errors.Wrapf(err, "nix did not build %s, saying: %s", flake, said)
	}
	out := strings.TrimSpace(stdout.String())
	if i := strings.LastIndexByte(out, '\n'); i >= 0 {
		out = out[i+1:]
	}
	binary := filepath.Join(out, "bin", "pi")
	if _, err := os.Stat(binary); err != nil {
		return "", errors.Wrapf(err, "nix built %s into %s, and it holds no bin/pi", flake, out)
	}
	return binary, nil
}
