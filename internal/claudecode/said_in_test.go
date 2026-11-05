package claudecode

// "another thing i want is that rituals run on the box from now on, not locally on my machine per se."

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkout makes a git checkout of owner/repo in the agent's work.
func checkout(t *testing.T, home, ownerRepo string) string {
	t.Helper()
	dir := filepath.Join(home, "work", filepath.FromSlash(ownerRepo))
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A turn said in a repository the agent keeps in its work runs in that checkout, so the hooks
// it fires stand where the repository's rituals are switched on (ground source/ritual/onoff.d).
// It resumes the session it is in: Claude Code finds a session from a directory under the one
// it was started in (measured with claude -p --resume from a subdirectory, 2026-11-05).
func TestATurnSaidInARepositoryOfItsWorkRunsInThatCheckout(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	grove := checkout(t, home, "sbvh-nl/grove")
	said := Said{Started: noted, Binary: binary, Home: home, Session: "s-1", Resumes: true, Says: "rituals on",
		Model: "m", Effort: "low", Mode: "dontAsk", In: "sbvh-nl/grove"}
	if _, err := said.Run(context.Background(), func(Message) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cwd := linesOf(t, filepath.Join(ran, "cwd"))[0]
	want, err := filepath.EvalSymlinks(grove)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.EvalSymlinks(cwd); got != want {
		t.Errorf("the turn ran in %s, not in the checkout %s", cwd, want)
	}
}

// A turn named in nothing is said in its work, as every turn was.
func TestATurnSaidInNothingRunsInItsWork(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	said := Said{Started: noted, Binary: binary, Home: home, Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk"}
	if _, err := said.Run(context.Background(), func(Message) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(home, "work"))
	if got, _ := filepath.EvalSymlinks(linesOf(t, filepath.Join(ran, "cwd"))[0]); got != want {
		t.Errorf("the turn ran in %s, not in its work %s", got, want)
	}
}

// Only a checkout of the agent's own work is a place a turn is said in: anything else is
// refused before Claude Code runs.
func TestATurnIsSaidInNoPlaceOutsideItsWorksCheckouts(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	checkout(t, home, "sbvh-nl/grove")
	if err := os.MkdirAll(filepath.Join(home, "work", "sbvh-nl", "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"..", "../x", "/etc", "sbvh-nl", "sbvh-nl/grove/controls", "../../x/y", "sbvh-nl/..", "sbvh-nl/absent", "sbvh-nl/plain"} {
		said := Said{Started: noted, Binary: binary, Home: home, Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk", In: in}
		_, err := said.Run(context.Background(), func(Message) {})
		if err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("a turn said in %q was not refused by name: %v", in, err)
		}
	}
	if _, err := os.Stat(filepath.Join(ran, "args")); err == nil {
		t.Error("Claude Code ran for a turn said in a place outside its work's checkouts")
	}
}

// Qodo on #1536: a name in the work that is a link to a checkout elsewhere is no checkout of its
// work, whichever part of owner/repo the link is.
func TestATurnIsNotSaidInACheckoutALinkTakesOutsideItsWork(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "work", "owner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, "work", "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "repo"), filepath.Join(home, "work", "owner", "repo")); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"linked/repo", "owner/repo"} {
		said := Said{Started: noted, Binary: binary, Home: home, Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk", In: in}
		if _, err := said.Run(context.Background(), func(Message) {}); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("a turn said in %q, a link out of its work, was not refused by name: %v", in, err)
		}
	}
	if _, err := os.Stat(filepath.Join(ran, "args")); err == nil {
		t.Error("Claude Code ran in a checkout outside its work")
	}
}
