package comet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// Source is a repository a comet is built from, at its branch's latest; an empty branch is its default.
type Source struct{ Owner, Repo, Branch string }

func (s Source) String() string { return s.Owner + "/" + s.Repo + "@" + s.Branch }

// Ground is where ground's own sources and global controls come from.
var Ground = Source{Owner: "teranos", Repo: "ground", Branch: "main"}

// Node is what a build asks of the node it runs on: where a source is now, its tarball,
// a directory to build in, and a step run.
type Node interface {
	Latest(ctx context.Context, source Source) (string, error)
	Fetch(ctx context.Context, source Source, rev, tarball string) error
	BuildsDir() (string, error)
	Run(ctx context.Context, dir string, env []string, name string, args ...string) error
}

// Builder builds comets on a node, with the nix at NixBin, every step at the lowest CPU priority.
type Builder struct {
	Node   Node
	NixBin string
}

// "one build at a time"

// building holds a comet's build while it runs (ADR-050, The builder).
var building sync.Mutex

// Build builds ground with its own controls and the project's, at both repositories' latest, and keeps it.
// Each comet builds in a directory of its own, emptied when its next build begins: a failed build stays to be read until then.
func (b Builder) Build(ctx context.Context, n Name) error {
	building.Lock()
	defer building.Unlock()
	builds, err := b.Node.BuildsDir()
	if err != nil {
		return err
	}
	work := filepath.Join(builds, "comet-"+n.Owner+"-"+n.Repo)
	if err := os.RemoveAll(work); err != nil {
		return errors.Wrapf(err, "the last build of the comet of %s in %s was not cleared", n.Key(), work)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s to build the comet of %s in", work, n.Key())
	}
	return b.buildIn(ctx, n, work)
}

// buildIn builds the comet in work and keeps it.
func (b Builder) buildIn(ctx context.Context, n Name, work string) error {
	checkout := filepath.Join(work, "project")
	rev, err := b.unpack(ctx, Source{Owner: n.Owner, Repo: n.Repo}, work, checkout)
	if err != nil {
		return err
	}
	kept, err := Path(n)
	if err != nil {
		return err
	}
	of := n.Owner + "/" + n.Repo + "@" + short(rev)
	return errors.Wrapf(b.ground(ctx, work, n.Platform, Declaration(checkout, n.Owner, n.Repo), of, kept), "the comet of %s did not build", n.Key())
}

// short is a rev as git describe names one.
func short(rev string) string { return rev[:min(len(rev), 7)] }

// Version is what a comet's rows carry after ground in their source column: which ground's main
// and which project rev it was built from, so the node knows which comet landed (ADR-050, Landing).
func Version(groundRev, of string) string { return "comet " + short(groundRev) + " " + of + "\n" }

// unpack is source at its latest, unpacked into dir, and that rev.
func (b Builder) unpack(ctx context.Context, source Source, work, dir string) (string, error) {
	rev, err := b.Node.Latest(ctx, source)
	if err != nil {
		return "", errors.Wrapf(err, "the latest of %s was not read", source.String())
	}
	tarball := dir + ".tar.gz"
	if err := b.Node.Fetch(ctx, source, rev, tarball); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s", dir)
	}
	return rev, errors.Wrapf(b.Node.Run(ctx, work, nil, "tar", "-xzf", tarball, "--strip-components=1", "-C", dir),
		"failed to unpack %s at %s", source.String(), rev)
}

// ground builds ground's main in work with sand beside its own controls, for platform, and keeps
// it at kept with its digest beside it. of names what the sand was built from, for its version.
func (b Builder) ground(ctx context.Context, work, platform, sand, of, kept string) error {
	src := filepath.Join(work, "ground")
	groundRev, err := b.unpack(ctx, Ground, work, src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(src, ".version"), []byte(Version(groundRev, of)), 0o644); err != nil {
		return errors.Wrapf(err, "failed to write the comet's version in %s", src)
	}
	local := filepath.Join(src, "controls", "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s", local)
	}
	if err := os.WriteFile(filepath.Join(local, "comet.pbt"), []byte(sand), 0o644); err != nil {
		return errors.Wrapf(err, "failed to write the comet's sand in %s", local)
	}
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return errors.Wrapf(err, "failed to make the comet's temp directory in %s", work)
	}

	env := []string{"PATH=" + b.NixBin + ":" + os.Getenv("PATH"), "TMPDIR=" + filepath.Join(work, "tmp")}
	native, err := Native()
	if err != nil {
		return err
	}
	if platform == native {
		if err := b.step(ctx, src, env, b.inGround(src, "make", "build")); err != nil {
			return err
		}
	}
	if platform != native {
		if err := b.buildDarwin(ctx, src, env, platform); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s", filepath.Dir(kept))
	}
	binary, err := os.ReadFile(filepath.Join(src, "ground"))
	if err != nil {
		return errors.Wrapf(err, "the built ground was not found in %s", src)
	}
	// Renamed into place, never written over: a GET reading the old one reads it whole.
	staged := kept + ".new"
	if err := os.WriteFile(staged, binary, 0o755); err != nil {
		return errors.Wrapf(err, "failed to write %s", staged)
	}
	if err := os.Rename(staged, kept); err != nil {
		return errors.Wrapf(err, "failed to put the built ground in place at %s", kept)
	}
	return KeepDigest(kept)
}

// inGround is args run in ground's own dev shell, the toolchain its flake.lock pins.
func (b Builder) inGround(src string, args ...string) []string {
	nix := filepath.Join(b.NixBin, "nix")
	return append([]string{"shell", "--inputs-from", src, "nixpkgs#gnumake", "-c", nix, "develop", ".#default", "--command"}, args...)
}

// step runs one nix invocation in src at nice 19, so the node it builds on keeps answering.
func (b Builder) step(ctx context.Context, src string, env []string, args []string) error {
	return b.Node.Run(ctx, src, env, "nice", append([]string{"-n", "19", filepath.Join(b.NixBin, "nix")}, args...)...)
}

// "A Darwin build on the box takes clang as cc, ld64.lld as linker, and Apple's link stubs"

// darwinTargets are the Darwin platforms a comet is linked for: LDC's triple and ld64's arch.
var darwinTargets = map[string]struct{ triple, arch string }{
	"aarch64-darwin": {"arm64-apple-macos14.0", "arm64"},
	"x86_64-darwin":  {"x86_64-apple-macos14.0", "x86_64"},
}

// DarwinStubs is what a Darwin comet links against: Apple's SDK stubs, and the libgit2 the
// machine it lands on loads, which no SDK carries.
type DarwinStubs struct{ SDK, Git2 string }

// Stubs are the stubs the node holds for platform, under ~/.qntx/darwin-stubs: the box's
// reconciler unpacks them there from the bootstrap bucket (ADR-050, The stubs).
func Stubs(platform string) (DarwinStubs, error) {
	if _, darwin := darwinTargets[platform]; !darwin {
		return DarwinStubs{}, errors.Newf("%s is not a platform this node links: it links Darwin alone", platform)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return DarwinStubs{}, errors.Wrap(err, "the node's home was not found, so its stubs were not looked for")
	}
	dir := filepath.Join(home, ".qntx", "darwin-stubs")
	stubs := DarwinStubs{SDK: filepath.Join(dir, "MacOSX.sdk"), Git2: filepath.Join(dir, platform, "libgit2.dylib")}
	for _, held := range []string{filepath.Join(stubs.SDK, "SDKSettings.json"), stubs.Git2} {
		info, err := os.Stat(held)
		if err != nil {
			return DarwinStubs{}, errors.Wrapf(err, "this node does not hold %s", held)
		}
		if info.IsDir() {
			return DarwinStubs{}, errors.Newf("%s is a directory, where a stub is a file", held)
		}
	}
	return stubs, nil
}

// buildDarwin builds ground in src for a Darwin platform: wind folds the sand, LDC compiles
// for the target's triple, and ld64.lld links against Apple's stubs. With -betterC ground
// links no druntime and no Phobos, so the stubs are the whole of what it links.
func (b Builder) buildDarwin(ctx context.Context, src string, env []string, platform string) error {
	target := darwinTargets[platform]
	stubs, err := Stubs(platform)
	if err != nil {
		return err
	}
	stamp := []byte(time.Now().UTC().Format("2006-01-02T15:04:05Z") + "\n")
	if err := os.WriteFile(filepath.Join(src, ".builddate"), stamp, 0o644); err != nil {
		return errors.Wrapf(err, "failed to write the build date in %s", src)
	}
	sources, err := Sources(src)
	if err != nil {
		return err
	}
	steps := [][]string{
		b.inGround(src, "make", "wind"),
		b.inGround(src, "tools/wind"),
		b.inGround(src, append([]string{"ldc2", "-betterC", "--lowmem", "-mtriple=" + target.triple, "-O", "-release",
			"-I=source", "-J=.", "-c", "--singleobj", "-of=ground.o"}, sources...)...),
		{"shell", "--inputs-from", src, "nixpkgs#lld_21", "-c", "ld64.lld", "-arch", target.arch,
			"-platform_version", "macos", "14.0", "14.0", "-syslibroot", stubs.SDK, "-o", "ground", "ground.o",
			"-lSystem", "-lobjc", "-lsqlite3", "-lcurl", "-lpthread", "-lm", stubs.Git2},
	}
	for _, args := range steps {
		if err := b.step(ctx, src, env, args); err != nil {
			return err
		}
	}
	return nil
}

// Sources are ground's sources as its production build takes them: every module under
// source/, and none of the tests (ground dub.json).
func Sources(src string) ([]string, error) {
	sources := []string{}
	err := filepath.WalkDir(filepath.Join(src, "source"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".d") || strings.HasSuffix(path, "_test.d") {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		sources = append(sources, rel)
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "ground's sources under %s were not listed", src)
	}
	return sources, nil
}
