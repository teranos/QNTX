package pi

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// nixStandIn stands in for nix: it writes down how it was run and prints the
// store path it was told to, after making it hold bin/pi when asked.
func nixStandIn(t *testing.T, out string, withPi bool, exit int) (nix, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "args")
	if withPi {
		if err := os.MkdirAll(filepath.Join(out, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "bin", "pi"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > '" + ran + "'\n" +
		"echo 'error: cannot build' >&2\n" +
		"printf '%s\\n' '" + out + "'\n" +
		"exit " + string(rune('0'+exit)) + "\n"
	nix = filepath.Join(dir, "nix")
	if err := os.WriteFile(nix, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return nix, ran
}

// The pinned flake is built by nix, and Pi is the binary it holds.
func TestEnsureBuildsThePinnedPi(t *testing.T) {
	out := filepath.Join(t.TempDir(), "store-pi-1.0.3")
	nix, ran := nixStandIn(t, out, true, 0)

	link := filepath.Join(t.TempDir(), "pi", "result")
	binary, err := Ensure(context.Background(), nix, PinnedFlake, link)
	if err != nil {
		t.Fatal(err)
	}
	if binary != filepath.Join(out, "bin", "pi") {
		t.Fatalf("binary = %s", binary)
	}
	args := linesOf(t, ran)
	if !slices.Contains(args, PinnedFlake) || !slices.Contains(args, "nix-command flakes") {
		t.Fatalf("nix ran with %v", args)
	}
	// The box collects its Nix store at every deploy: Pi is rooted, and no
	// cached evaluation names what a collection deleted.
	if after(args, "--out-link") != link || !slices.Contains(args, "--no-eval-cache") || slices.Contains(args, "--no-link") {
		t.Fatalf("nix ran with %v, Pi unrooted or evaluated from cache", args)
	}
	// Pi comes from the cache CI fills, and is never built on the node.
	if after(args, "--max-jobs") != "0" {
		t.Fatalf("nix ran with %v, free to build Pi on the node", args)
	}
	if !strings.Contains(PinnedFlake, "d78dc83d633229d12f8b79631384c4c2717c399f") {
		t.Fatalf("the pin is not a commit: %s", PinnedFlake)
	}
}

// A build that failed says so in nix's words, and one that built nothing
// runnable says what it built.
func TestEnsureSaysWhyThereIsNoPi(t *testing.T) {
	nix, _ := nixStandIn(t, filepath.Join(t.TempDir(), "x"), false, 1)
	if _, err := Ensure(context.Background(), nix, PinnedFlake, filepath.Join(t.TempDir(), "result")); err == nil || !strings.Contains(err.Error(), "cannot build") {
		t.Fatalf("err = %v", err)
	}
	nix, _ = nixStandIn(t, filepath.Join(t.TempDir(), "empty"), false, 0)
	if _, err := Ensure(context.Background(), nix, PinnedFlake, filepath.Join(t.TempDir(), "result")); err == nil || !strings.Contains(err.Error(), "holds no bin/pi") {
		t.Fatalf("err = %v", err)
	}
}
