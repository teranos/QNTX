package comet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	errors "github.com/teranos/sacred-error"
)

// Source is a repository a comet is built from, at its branch's latest; an empty branch is its default.
type Source struct{ Owner, Repo, Branch string }

func (s Source) String() string { return s.Owner + "/" + s.Repo + "@" + s.Branch }

// Ground is where ground's own sources and global controls come from: libacp
// while ground's main is held, since `make sand` is there and not on main.
// "12 for now yes"
var Ground = Source{Owner: "teranos", Repo: "ground", Branch: "libacp"}

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

// buildIn builds the comet in work and keeps it, under the engine ABI the sand was built for.
func (b Builder) buildIn(ctx context.Context, n Name, work string) error {
	checkout := filepath.Join(work, "project")
	rev, err := b.unpack(ctx, Source{Owner: n.Owner, Repo: n.Repo}, work, checkout)
	if err != nil {
		return err
	}
	// "we can maintain the pbt for the ground comets that impact this laptop on l"
	machineOwner, machineRepo, named := strings.Cut(n.Machine, "/")
	if !named {
		return errors.Newf("the comet of %s names no machine as owner/repo: %q", n.Key(), n.Machine)
	}
	machine := filepath.Join(work, "machine")
	machineRev, err := b.unpack(ctx, Source{Owner: machineOwner, Repo: machineRepo}, work, machine)
	if err != nil {
		return err
	}
	sand, err := machineSand(machine, n.Machine)
	if err != nil {
		return err
	}
	sand["comet.pbt"] = Declaration(checkout, n.Owner, n.Repo)
	of := n.Owner + "/" + n.Repo + "@" + short(rev) + " " + n.Machine + "@" + short(machineRev)
	// "only the compiled controls being their own thing shipped to the user"
	// A comet is the sand alone, loaded by the ground that lands it (ground source/sandload.d).
	src, env, err := b.prepare(ctx, work, sand, of)
	if err != nil {
		return errors.Wrapf(err, "the comet of %s did not build", n.Base())
	}
	abi, err := b.sandIn(ctx, src, env, n.Platform)
	if err != nil {
		return errors.Wrapf(err, "the comet of %s did not build", n.Base())
	}
	n.ABI = abi
	kept, err := Path(n)
	if err != nil {
		return err
	}
	if err := keepBuilt(src, filepath.Join("build", SandFile(n.Platform)), kept); err != nil {
		return err
	}
	return errors.Wrapf(os.WriteFile(kept+".machine", []byte(n.Machine), 0o644), "the comet of %s was kept, and the machine it carries was not written beside it", n.Key())
}

// machineSand is each pbt in the machine repository's controls/, by the name it lands under in
// ground's controls/local: a machine's controls are the ones that stay on it. Its qntx.pbt names
// the node the comet reports to and lands from, so a machine without one is no machine.
func machineSand(machine, named string) (map[string]string, error) {
	node := filepath.Join(machine, "controls", "qntx.pbt")
	info, err := os.Stat(node)
	if err != nil {
		return nil, errors.Wrapf(err, "%s holds no controls/qntx.pbt, so it names no node for its comet", named)
	}
	if info.IsDir() {
		return nil, errors.Newf("%s's controls/qntx.pbt is a directory, where the node is named in a file", named)
	}
	found, err := filepath.Glob(filepath.Join(machine, "controls", "*.pbt"))
	if err != nil {
		return nil, errors.Wrapf(err, "the controls of %s were not listed", named)
	}
	sand := map[string]string{}
	for _, path := range found {
		pbt, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "the control %s of %s was not read", path, named)
		}
		sand["machine-"+filepath.Base(path)] = string(pbt)
	}
	return sand, nil
}

// short is a rev as git describe names one.
func short(rev string) string { return rev[:min(len(rev), 7)] }

// Version is what a comet's rows carry after ground in their source column: which ground's branch
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

// prepare unpacks ground's branch in work with sand beside its own controls, and answers where, and
// the environment its steps run in. of names what the sand was built from, for its version.
func (b Builder) prepare(ctx context.Context, work string, sand map[string]string, of string) (string, []string, error) {
	src := filepath.Join(work, "ground")
	groundRev, err := b.unpack(ctx, Ground, work, src)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(src, ".version"), []byte(Version(groundRev, of)), 0o644); err != nil {
		return "", nil, errors.Wrapf(err, "failed to write the comet's version in %s", src)
	}
	local := filepath.Join(src, "controls", "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		return "", nil, errors.Wrapf(err, "failed to make %s", local)
	}
	for name, pbt := range sand {
		if err := os.WriteFile(filepath.Join(local, name), []byte(pbt), 0o644); err != nil {
			return "", nil, errors.Wrapf(err, "failed to write the comet's %s in %s", name, local)
		}
	}
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return "", nil, errors.Wrapf(err, "failed to make the comet's temp directory in %s", work)
	}
	return src, []string{"PATH=" + b.NixBin + ":" + os.Getenv("PATH"), "TMPDIR=" + filepath.Join(work, "tmp")}, nil
}

// ground builds ground's branch in work with sand beside its own controls, natively, and keeps
// each binary kept names at its path, with its digest beside it.
func (b Builder) ground(ctx context.Context, work string, sand map[string]string, of string, kept map[string]string) error {
	src, env, err := b.prepare(ctx, work, sand, of)
	if err != nil {
		return err
	}
	for name, path := range kept {
		if err := b.step(ctx, src, env, b.inGround(src, "make", goalOf(name))); err != nil {
			return err
		}
		if err := keepBuilt(src, name, path); err != nil {
			return err
		}
	}
	return nil
}

// sandIn builds the sand in src for platform at build/<SandFile>, and answers the engine ABI it is for.
// make sand builds it natively and says its ABI; the ABI is one layout hash whichever 64-bit platform
// ground's flake names, so a sand for another is compiled for its triple and linked with nothing.
func (b Builder) sandIn(ctx context.Context, src string, env []string, platform string) (string, error) {
	native, err := Native()
	if err != nil {
		return "", err
	}
	if err := b.step(ctx, src, env, b.inGround(src, "make", "sand")); err != nil {
		return "", err
	}
	abi, err := BuiltABI(src)
	if err != nil {
		return "", err
	}
	if platform != native {
		steps := [][]string{
			b.inGround(src, sandCompiled(platform)...),
			append([]string{"shell", "--inputs-from", src, "nixpkgs#lld_21", "-c"}, sandLinked(platform)...),
		}
		for _, args := range steps {
			if err := b.step(ctx, src, env, args); err != nil {
				return "", err
			}
		}
	}
	return abi, nil
}

// keepBuilt puts the binary built in src as name at kept, with its digest beside it.
func keepBuilt(src, name, kept string) error {
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s", filepath.Dir(kept))
	}
	binary, err := os.ReadFile(filepath.Join(src, name))
	if err != nil {
		return errors.Wrapf(err, "the built %s was not found in %s", name, src)
	}
	// Renamed into place, never written over: a GET reading the old one reads it whole.
	staged := kept + ".new"
	if err := os.WriteFile(staged, binary, 0o755); err != nil {
		return errors.Wrapf(err, "failed to write %s", staged)
	}
	if err := os.Rename(staged, kept); err != nil {
		return errors.Wrapf(err, "failed to put the built %s in place at %s", name, kept)
	}
	return KeepDigest(kept)
}

// goalOf is the make goal that builds a binary of ground's natively.
func goalOf(name string) string {
	if name == "ground" {
		return "build"
	}
	return name
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

// darwinTargets are the Darwin platforms a sand is linked for: LDC's triple and ld64's arch.
var darwinTargets = map[string]struct{ triple, arch string }{
	"aarch64-darwin": {"arm64-apple-macos14.0", "arm64"},
	"x86_64-darwin":  {"x86_64-apple-macos14.0", "x86_64"},
}

// linuxTargets are the Linux platforms a sand is linked for: LDC's triple.
var linuxTargets = map[string]string{
	"aarch64-linux": "aarch64-linux-gnu",
	"x86_64-linux":  "x86_64-linux-gnu",
}

// sandCompiled is the LDC invocation that compiles the sand for platform into build/sand.o, as
// ground's make sand compiles it: bin/sand.d with builtin_sand and sandview alone.
func sandCompiled(platform string) []string {
	triple := linuxTargets[platform]
	pic := []string{"-relocation-model=pic"}
	if target, darwin := darwinTargets[platform]; darwin {
		triple, pic = target.triple, nil
	}
	args := []string{"ldc2", "-betterC", "--lowmem", "-mtriple=" + triple, "-O", "-release",
		"-fvisibility=hidden", "--function-sections", "--data-sections"}
	args = append(args, pic...)
	return append(args, "-I=source", "-J=.", "-i=builtin_sand", "-i=sandview", "-c", "--singleobj", "-of=build/sand.o", "bin/sand.d")
}

// sandLinked links build/sand.o into the library platform loads, against nothing.
func sandLinked(platform string) []string {
	out := filepath.Join("build", SandFile(platform))
	if target, darwin := darwinTargets[platform]; darwin {
		return []string{"ld64.lld", "-dylib", "-arch", target.arch, "-platform_version", "macos", "14.0", "14.0",
			"-dead_strip", "-o", out, "build/sand.o"}
	}
	return []string{"ld.lld", "-shared", "--gc-sections", "-z", "defs", "-o", out, "build/sand.o"}
}
