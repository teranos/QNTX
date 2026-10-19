package server

// QNTX builds a plugin itself (ADR-002): from the sources its record names, as
// their branches are now, with the node's nix, and runs what it built.
// "it is qntx that actually owns this"

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// buildConfigPrefix marks the keys of a plugin's Config that are QNTX's.
const buildConfigPrefix = "build."

// The keys of a plugin's Config that say how QNTX builds it.
const (
	buildCore      = "build.core"
	buildInputs    = "build.inputs"
	buildInputsEnv = "build.inputs_env"
	buildPackages  = "build.packages"
	buildCommand   = "build.command"
	buildOutput    = "build.output"
)

// nixBins are where a node's nix is, as it was put there: by nix's installer (q, a Mac),
// or by NixOS (k), which keeps no default profile. Neither is on every service's PATH.
var nixBins = []string{"/nix/var/nix/profiles/default/bin", "/run/current-system/sw/bin"}

// nixBin is where the node's nix is: the first of nixBins holding one. A node with none
// is told so at start (nixIsNowhere), and a build asks the first and is refused there.
var nixBin = nixHeldAt(nixBins)

// nixHeldAt is the first of dirs holding nix, or the first of them when none does.
func nixHeldAt(dirs []string) string {
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "nix")); err == nil {
			return dir
		}
	}
	return dirs[0]
}

// nixIsNowhere says a node holds no nix in any of nixBins, and is empty on a node that holds one.
func nixIsNowhere() string {
	if _, err := os.Stat(filepath.Join(nixBin, "nix")); err == nil {
		return ""
	}
	return "this node holds no nix in " + strings.Join(nixBins, " nor ") + ", so nothing it builds (plugins, comets, an agent's ground) is built"
}

// buildSource is one repository a build takes from, at a branch. An input
// names a single file of it in Path; the source that is built names none.
type buildSource struct {
	Repo         forge.Repo
	Branch, Path string
	PathNamed    bool
}

func (b buildSource) String() string {
	s := b.Repo.String() + "@" + b.Branch
	if b.PathNamed {
		s += ":" + b.Path
	}
	return s
}

// TODO(forge): gitHubRepo and onGitHub are the adapter's. Move them to forge_github.go; outside it, only github_follow.go still calls onGitHub, and buildSource.onGitHub below has no caller.
// gitHubRepo is a repository as GitHub's API names it: an owner and a name.
type gitHubRepo struct{ Owner, Name string }

func (g gitHubRepo) String() string { return g.Owner + "/" + g.Name }

// onGitHub is the repository as GitHubService is asked about it. A repository
// on another forge is refused here, naming it, and not asked of GitHub.
func onGitHub(repo forge.Repo) (gitHubRepo, error) {
	if !strings.EqualFold(repo.Host, forge.GitHub) {
		return gitHubRepo{}, errors.Newf("%s is not on %s, and this asks GitHub", repo, forge.GitHub)
	}
	owner, name, ok := strings.Cut(repo.Path, "/")
	if !ok || strings.Contains(name, "/") {
		return gitHubRepo{}, errors.Newf("%s is not owner/repo: GitHub does not nest", repo)
	}
	return gitHubRepo{Owner: owner, Name: name}, nil
}

// TODO(forge): No caller. Remove.
// onGitHub is the source's repository as GitHubService is asked about it.
func (b buildSource) onGitHub() (gitHubRepo, error) { return onGitHub(b.Repo) }

// parseBuildSource reads host/path@branch, with :path when a file is named, as
// github.com/owner/repo@main:notes. Each part is named, or the source is
// refused naming the part that is not.
func parseBuildSource(s string, withPath bool) (buildSource, error) {
	repoText, at, ok := strings.Cut(strings.TrimSpace(s), "@")
	if !ok {
		return buildSource{}, errors.Newf("%q names no branch: host/path@branch", s)
	}
	file := buildSource{PathNamed: withPath}
	if withPath {
		var path string
		at, path, ok = strings.Cut(at, ":")
		if !ok {
			return buildSource{}, errors.Newf("%q names no file: host/path@branch:path", s)
		}
		pathText, err := named.TextOf("the file", path)
		if err != nil {
			return buildSource{}, errors.Wrapf(err, "%q names no file: host/path@branch:path", s)
		}
		file.Path = pathText.String()
	}
	branch, err := named.TextOf("the branch", at)
	if err != nil {
		return buildSource{}, errors.Wrapf(err, "%q names no branch: host/path@branch", s)
	}
	repo, err := forge.ParseRepo(repoText)
	if err != nil {
		return buildSource{}, errors.Wrapf(err, "%q names no repository", s)
	}
	file.Repo, file.Branch = repo, branch.String()
	return file, nil
}

// pluginBuild is how one plugin is built, as its record says.
type pluginBuild struct {
	name   string
	core   buildSource
	inputs []buildSource
	// inputsEnv is the variable the build reads its inputs' files from, and
	// inputsEnvNamed whether the record names one.
	inputsEnv      string
	inputsEnvNamed bool
	packages       []string
	command        string
	output         string
}

// buildOf is the build a record names. False is a plugin QNTX does not build.
func buildOf(record grpcplugin.PluginRecord) (pluginBuild, bool, error) {
	// A record naming no build.core is a plugin QNTX does not build; one naming
	// an empty one names no source, and parseBuildSource refuses it.
	core, ok := record.Config[buildCore]
	if !ok {
		return pluginBuild{}, false, nil
	}
	inputsEnv, inputsEnvNamed := record.Config[buildInputsEnv]
	// A build runs the command its record names and installs the output it
	// names. One naming an empty command builds nothing, and an empty output is
	// no binary: the build is refused where the binary is read.
	command, commandNamed := record.Config[buildCommand]
	if !commandNamed {
		return pluginBuild{}, false, errors.Newf("plugin %s names no %s", record.Name, buildCommand)
	}
	output, outputNamed := record.Config[buildOutput]
	if !outputNamed {
		return pluginBuild{}, false, errors.Newf("plugin %s names no %s", record.Name, buildOutput)
	}
	b := pluginBuild{
		name:           record.Name,
		inputsEnv:      inputsEnv,
		inputsEnvNamed: inputsEnvNamed,
		packages:       strings.Fields(record.Config[buildPackages]),
		command:        strings.TrimSpace(command),
		output:         strings.TrimSpace(output),
	}
	var err error
	if b.core, err = parseBuildSource(core, false); err != nil {
		return pluginBuild{}, false, errors.Wrapf(err, "plugin %s: %s", record.Name, buildCore)
	}
	for _, in := range strings.Fields(record.Config[buildInputs]) {
		source, err := parseBuildSource(in, true)
		if err != nil {
			return pluginBuild{}, false, errors.Wrapf(err, "plugin %s: %s", record.Name, buildInputs)
		}
		// An input is handed to the build in the variable the record names.
		if !inputsEnvNamed {
			return pluginBuild{}, false, errors.Newf("plugin %s takes %s and names no %s", record.Name, buildInputs, buildInputsEnv)
		}
		b.inputs = append(b.inputs, source)
	}
	return b, true, nil
}

// enabledBuilds is each enabled plugin QNTX builds, and why any record's build,
// by plugin, is not one QNTX can run.
func enabledBuilds(records []grpcplugin.PluginRecord) ([]pluginBuild, map[string]error) {
	var builds []pluginBuild
	refused := map[string]error{}
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		b, built, err := buildOf(record)
		if err != nil {
			refused[record.Name] = err
			continue
		}
		if built {
			builds = append(builds, b)
		}
	}
	return builds, refused
}

// builtRevsFile is beside a build QNTX installed: the revs it was built from.
const builtRevsFile = "built-revs"

// installedRevs is what name's installed build was built from. None is a
// plugin with no binary, or one whose binary QNTX did not build: false, for
// whatever reason, is a plugin built when its sources are next read.
func installedRevs(name string) ([]string, bool) {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return nil, false
	}
	if info, err := os.Stat(filepath.Join(dir, grpcplugin.PluginBinaryName(name))); err != nil || info.IsDir() {
		return nil, false
	}
	held, err := os.ReadFile(filepath.Join(dir, builtRevsFile))
	if err != nil {
		return nil, false
	}
	return strings.Fields(string(held)), true
}

// keepBuiltRevs writes what name's installed build was built from beside it.
func keepBuiltRevs(name string, revs []string) error {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, builtRevsFile), []byte(strings.Join(revs, " ")+"\n"), 0o644); err != nil {
		return errors.Wrapf(err, "the revs of %s's build were not kept", name)
	}
	return nil
}

// failedBuildFile is beside a plugin's install: the build that last failed,
// as failedKey writes it.
const failedBuildFile = "failed-build"

// failedKey is one build as it would run again: the revs, and the record's
// recipe, so a record changed at the same revs is a different build.
func (b pluginBuild) failedKey(revs []string) string {
	return strings.Join([]string{strings.Join(revs, " "), b.command, strings.Join(b.packages, " "), b.output, b.inputsEnv}, "\n") + "\n"
}

// failedBefore is whether this exact build already failed.
func failedBefore(name, key string) bool {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return false
	}
	held, err := os.ReadFile(filepath.Join(dir, failedBuildFile))
	return err == nil && string(held) == key
}

// clearFailed removes which build failed from beside the install: a built one
// clears it.
func clearFailed(name string) error {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, failedBuildFile)); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "the failed build of %s was not cleared", name)
	}
	return nil
}

// keepFailed writes which build failed beside the install.
func keepFailed(name, key string) error {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s to keep %s's failed build in", dir, name)
	}
	if err := os.WriteFile(filepath.Join(dir, failedBuildFile), []byte(key), 0o644); err != nil {
		return errors.Wrapf(err, "the failed build of %s was not kept", name)
	}
	return nil
}

// BuildMoved builds each enabled plugin whose sources are not what its
// installed build was built from: a restart stops a running build, and a
// push while the node is down reaches nobody.
func (s *QNTXServer) BuildMoved() {
	logger := s.logger.Named("build")
	records, err := s.pluginRecords().Plugins()
	if err != nil {
		logger.Errorw("The plugin records were not read, so no moved plugin is built", "error", err)
		return
	}
	builds, refused := enabledBuilds(records)
	// A build QNTX cannot run is said where the builds are shown, and the
	// others are built all the same.
	for name, err := range refused {
		s.builds.set(name, PluginBuildState{At: time.Now(), Error: "this build is not one QNTX can run: " + err.Error()})
	}
	for _, b := range builds {
		if revs, built := installedRevs(b.name); built {
			s.builds.set(b.name, PluginBuildState{Revs: revs, At: time.Now()})
			s.builds.built(b.name, revs)
		}
		sacred.Go("plugin.build."+b.name, func() {
			s.building.Lock()
			defer s.building.Unlock()
			s.keepStoreAlive(s.lifetime(), b.name, logger)
			s.buildIfMoved(s.lifetime(), b, logger)
			s.keepStoreAlive(s.lifetime(), b.name, logger)
		})
	}
}

// sources is every source of the build, the core first.
func (b pluginBuild) sources() []buildSource {
	return append([]buildSource{b.core}, b.inputs...)
}

// PluginBuildState is one plugin's builds as the GitHub element shows them.
type PluginBuildState struct {
	Revs    []string  `json:"revs"`
	Digest  string    `json:"digest,omitempty"`
	At      time.Time `json:"at"`
	Changed bool      `json:"changed"`
	Error   string    `json:"error,omitempty"`
}

// pluginBuilds is what the builds know between rounds: each plugin's last
// round as the GitHub element shows it, and the revs its installed build was
// built from.
type pluginBuilds struct {
	state     sync.Map // plugin name → PluginBuildState
	installed sync.Map // plugin name → its revs, space separated
}

func (p *pluginBuilds) set(name string, state PluginBuildState) {
	p.state.Store(name, state)
}

// built is that name's installed build was built from revs.
func (p *pluginBuilds) built(name string, revs []string) {
	p.installed.Store(name, strings.Join(revs, " "))
}

// builtFrom is whether name's installed build was built from revs.
func (p *pluginBuilds) builtFrom(name string, revs []string) bool {
	held, ok := p.installed.Load(name)
	if !ok {
		return false
	}
	builtRevs, isRevs := held.(string)
	return isRevs && builtRevs == strings.Join(revs, " ")
}

// all is every plugin's last round. Only set stores in state, so each entry
// is a plugin's name and its PluginBuildState.
func (p *pluginBuilds) all() map[string]PluginBuildState {
	out := map[string]PluginBuildState{}
	p.state.Range(func(key, value any) bool {
		name, isName := key.(string)
		state, isState := value.(PluginBuildState)
		if isName && isState {
			out[name] = state
		}
		return true
	})
	return out
}

// buildIfMoved asks GitHub where every source of b is now, and builds when
// that is not what was last built.
func (s *QNTXServer) buildIfMoved(ctx context.Context, b pluginBuild, logger *zap.SugaredLogger) {
	var revs []string
	for _, source := range b.sources() {
		rev, err := s.buildRev(ctx, source)
		if err != nil {
			// Said where the builds are shown, and mailed to ROOT.
			s.builds.set(b.name, PluginBuildState{At: time.Now(),
				Error: "the rev of " + source.String() + " was not read, so the plugin is not built: " + err.Error()})
			s.deploys.fail(b.name, err)
			s.mailBuildFailure(ctx, b, nil, err, logger)
			return
		}
		revs = append(revs, rev)
	}
	if s.builds.builtFrom(b.name, revs) {
		return
	}
	deploy := pluginDeploy{plugin: b.name, rev: revs[0], revs: revs, stage: deployBuilding}
	if replaces, built := installedRevs(b.name); built {
		deploy.replacesRevs = replaces
	}
	s.deploys.begin(deploy)
	// A build that failed fails again at the same revs with the same recipe,
	// and ROOT was mailed the first time.
	key := b.failedKey(revs)
	if failedBefore(b.name, key) {
		logger.Infow("Not building again: this build failed before, and ROOT was told", "plugin", b.name, "revs", revs)
		s.builds.set(b.name, PluginBuildState{Revs: revs, At: time.Now(), Error: "this build failed before at these revs; a push or a change to its record builds it again"})
		s.deploys.fail(b.name, errors.New("this build failed before at these revs"))
		return
	}

	logger.Infow("Building a plugin whose sources moved", "plugin", b.name, "revs", revs)
	state := PluginBuildState{Revs: revs, At: time.Now()}
	done, err := s.buildPlugin(ctx, b, revs)
	state.Changed, state.Digest = done.changed, done.digest
	// What did not follow the build is said where the builds are shown.
	unkept := done.unkept
	if err != nil && s.stoppedWithTheNode(ctx) {
		// Not a failure of the build: the next start builds it.
		logger.Infow("A plugin's build stopped with the node, and the next start builds it", "plugin", b.name, "revs", revs)
		state.Error = strings.Join(append([]string{"the build stopped with the node: " + err.Error()}, unkept...), "; ")
		s.builds.set(b.name, state)
		return
	}
	if err != nil && interrupted(err) {
		// Not a failure of the build either: the next push or start builds it.
		logger.Infow("A plugin's build was interrupted, and the next push or start builds it", "plugin", b.name, "revs", revs, "error", err)
		state.Error = strings.Join(append([]string{"the build was interrupted: " + err.Error()}, unkept...), "; ")
		s.builds.set(b.name, state)
		s.deploys.interrupt(b.name, err)
		return
	}
	// A failed build is said where the builds are shown, and mailed to ROOT.
	if err != nil {
		if keptErr := keepFailed(b.name, key); keptErr != nil {
			unkept = append(unkept, "and it was not kept as failed, so the next start builds and mails it again: "+keptErr.Error())
		}
		state.Error = strings.Join(append([]string{err.Error()}, unkept...), "; ")
		s.builds.set(b.name, state)
		s.deploys.fail(b.name, err)
		s.mailBuildFailure(ctx, b, revs, err, logger)
		return
	}
	s.builds.built(b.name, revs)
	if err := keepBuiltRevs(b.name, revs); err != nil {
		unkept = append(unkept, "what it was built from was not kept, so the next start builds it again: "+err.Error())
	}
	if err := clearFailed(b.name); err != nil {
		unkept = append(unkept, "its earlier failure was not cleared: "+err.Error())
	}
	state.Error = strings.Join(unkept, "; ")
	s.builds.set(b.name, state)
	if !done.changed {
		s.deploys.unchanged(b.name)
		return
	}
	s.buildLanded(b.name)
}

// stoppedWithTheNode reports whether a build ended because the node is
// stopping: systemd signals the build with the node, before its context ends.
// A deploy stops the node, and a build cut off by it says nothing of the plugin.
func (s *QNTXServer) stoppedWithTheNode(ctx context.Context) bool {
	if err := ctx.Err(); err != nil {
		return true
	}
	return s.getState() != ServerStateRunning
}

// interrupted is whether a build step was ended from outside it: by a signal,
// or by nix saying it was interrupted, which it does on a signal of its own.
func interrupted(err error) bool {
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == -1 {
		return true
	}
	return strings.Contains(err.Error(), "error: interrupted by the user")
}

// buildRev is the commit source's branch points at now, or the last commit
// that touched its file.
func (s *QNTXServer) buildRev(ctx context.Context, source buildSource) (string, error) {
	reader, err := s.forges().For(source.Repo)
	if err != nil {
		return "", err
	}
	return reader.Head(ctx, forge.AsNode, source.Repo, source.Branch, source.Path)
}

// buildsDir is where builds work: on disk under the node's home.
func buildsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "the node's home was not found, so there is nowhere to build")
	}
	dir := filepath.Join(home, ".qntx", "builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s to build in", dir)
	}
	return dir, nil
}

// buildEnv is what a build runs with: the node's nix, its inputs, and its
// temp files inside its own work directory.
func buildEnv(work string, b pluginBuild, files []string) []string {
	env := []string{
		"PATH=" + nixBin + ":" + os.Getenv("PATH"),
		"TMPDIR=" + filepath.Join(work, "tmp"),
	}
	if b.inputsEnvNamed {
		env = append(env, b.inputsEnv+"="+strings.Join(files, " "))
	}
	return env
}

// gentle runs a build at the lowest CPU priority, so the node it builds on
// keeps answering.
func gentle(name string, args []string) (string, []string) {
	return "nice", append([]string{"-n", "19", name}, args...)
}

// builtPlugin is what one build did: whether the installed binary changed, its
// digest, and what did not follow the build.
type builtPlugin struct {
	changed bool
	digest  string
	unkept  []string
}

// buildPlugin builds b from revs in a directory of its own, packages what it
// built, and installs it. The directory goes whatever the build did, and one
// left behind is said in unkept.
func (s *QNTXServer) buildPlugin(ctx context.Context, b pluginBuild, revs []string) (builtPlugin, error) {
	builds, err := buildsDir()
	if err != nil {
		return builtPlugin{}, err
	}
	work, err := os.MkdirTemp(builds, b.name+"-")
	if err != nil {
		return builtPlugin{}, errors.Wrapf(err, "failed to make a directory in %s to build in", builds)
	}
	var done builtPlugin
	done.changed, done.digest, err = s.buildIn(ctx, b, revs, work)
	if removeErr := os.RemoveAll(work); removeErr != nil {
		done.unkept = append(done.unkept, "its build directory "+work+" was not removed: "+removeErr.Error())
	}
	return done, err
}

// buildIn builds b from revs in work.
func (s *QNTXServer) buildIn(ctx context.Context, b pluginBuild, revs []string, work string) (bool, string, error) {
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return false, "", errors.Wrapf(err, "failed to make the build's temp directory in %s", work)
	}

	// The core comes through the node's GitHub, as the inputs do: a private
	// repository has no public archive to fetch.
	src := filepath.Join(work, "src")
	tarball := filepath.Join(work, "core.tar.gz")
	if err := s.fetchCore(ctx, b.core, revs[0], tarball); err != nil {
		return false, "", err
	}
	if err := os.Mkdir(src, 0o755); err != nil {
		return false, "", errors.Wrapf(err, "failed to make %s", src)
	}
	// GitHub's archive holds one directory named for the repository and rev.
	if err := runBuild(ctx, work, nil, "tar", "-xzf", tarball, "--strip-components=1", "-C", src); err != nil {
		return false, "", errors.Wrapf(err, "failed to unpack %s at %s", b.core.String(), revs[0])
	}

	var files []string
	for i, in := range b.inputs {
		file, err := s.fetchInput(ctx, in, revs[i+1], work)
		if err != nil {
			return false, "", err
		}
		files = append(files, file)
	}

	args := []string{"shell", "--inputs-from", src}
	for _, p := range b.packages {
		args = append(args, "nixpkgs#"+p)
	}
	args = append(args, "-c", "sh", "-c", b.command)
	nice, niceArgs := gentle(filepath.Join(nixBin, "nix"), args)
	if err := runBuild(ctx, src, buildEnv(work, b, files), nice, niceArgs...); err != nil {
		return false, "", errors.Wrapf(err, "the build of %s at %s failed", b.name, revs[0])
	}

	// The version coming in is the built binary's own word for it.
	binary := filepath.Join(src, b.output)
	incoming, err := builtVersion(ctx, b.name, binary)
	if err != nil {
		return false, "", err
	}
	s.deploys.incoming(b.name, incoming)

	archive, err := grpcplugin.PackageBuild(b.name, binary)
	if err != nil {
		return false, "", err
	}
	return grpcplugin.InstallBuild(b.name, archive, s.logger.Named("build"))
}

// builtVersion is what a built plugin says its version is: every plugin
// answers --version with qntx-<name>-plugin <version> (plugin/grpc/run.go).
func builtVersion(ctx context.Context, name, binary string) (string, error) {
	said, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return "", errors.Wrapf(err, "%s --version did not answer", binary)
	}
	first := strings.SplitN(string(said), "\n", 2)[0]
	version, ok := strings.CutPrefix(strings.TrimSpace(first), grpcplugin.PluginBinaryName(name)+" ")
	if !ok {
		return "", errors.Newf("%s --version said %q, not %s <version>", binary, first, grpcplugin.PluginBinaryName(name))
	}
	return version, nil
}

// fetchCore writes the core's archive at rev to path.
func (s *QNTXServer) fetchCore(ctx context.Context, core buildSource, rev, path string) error {
	reader, err := s.forges().For(core.Repo)
	if err != nil {
		return err
	}
	body, err := reader.Archive(ctx, forge.AsNode, core.Repo, rev)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch %s at %s", core.String(), rev)
	}
	defer func() { sqlclose.Log(body.Close(), s.logger, "the archive of "+core.String()) }()
	file, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "failed to make %s", path)
	}
	if written, err := io.Copy(file, body); err != nil {
		sqlclose.Log(file.Close(), s.logger, path)
		return errors.Wrapf(err, "failed to write %s at %s to %s after %d bytes", core.String(), rev, path, written)
	}
	if err := file.Close(); err != nil {
		return errors.Wrapf(err, "failed to finish writing %s", path)
	}
	return nil
}

// fetchInput writes one input file at rev into work, and says where.
func (s *QNTXServer) fetchInput(ctx context.Context, in buildSource, rev, work string) (string, error) {
	reader, err := s.forges().For(in.Repo)
	if err != nil {
		return "", err
	}
	data, err := reader.File(ctx, forge.AsNode, in.Repo, rev, in.Path)
	if err != nil {
		return "", errors.Wrapf(err, "%s", in.String())
	}
	dir := filepath.Join(work, "inputs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s", dir)
	}
	file := filepath.Join(dir, strings.ReplaceAll(in.Repo.Path, "/", "-")+"-"+filepath.Base(in.Path))
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return "", errors.Wrapf(err, "failed to write %s", file)
	}
	return file, nil
}

// runBuild runs one step of a build in dir. A step that fails says what it
// printed last.
func runBuild(ctx context.Context, dir string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		said := errOut.String()
		if len(said) > 2000 {
			said = said[len(said)-2000:]
		}
		return errors.Wrapf(err, "%s %s: %s", name, strings.Join(args, " "), said)
	}
	return nil
}

// buildFailureMail is the mail a failed build is, to the ROOT User.
func buildFailureMail(b pluginBuild, revs []string, failed error) services.NodeMail {
	var text strings.Builder
	fmt.Fprintf(&text, "%s did not build.\n\n", b.name)
	for i, source := range b.sources() {
		rev := "not read"
		if i < len(revs) {
			rev = revs[i]
		}
		fmt.Fprintf(&text, "%s at %s\n", source.String(), rev)
	}
	fmt.Fprintf(&text, "\n%s\n", failed.Error())
	return services.NodeMail{
		Name:    "build.failed",
		Subject: b.name + " did not build",
		Bodies:  []services.MailBody{services.TextBody(text.String()), services.HTMLBody("<pre>" + html.EscapeString(text.String()) + "</pre>")},
	}
}

// mailBuildFailure mails ROOT that b did not build, and says so when it cannot.
func (s *QNTXServer) mailBuildFailure(ctx context.Context, b pluginBuild, revs []string, failed error, logger *zap.SugaredLogger) {
	// A node with no login keeps no Users, and says so here.
	root, found, err := s.authHandler.RootUser()
	if err != nil || !found {
		logger.Errorw("A plugin did not build and there is no ROOT User to mail", "plugin", b.name, "error", err)
		return
	}
	messageID, attestationID, err := s.nodeMailer.SendAsNode(ctx, root.ID, buildFailureMail(b, revs, failed))
	if err != nil {
		logger.Errorw("A plugin did not build and the mail saying so was not sent", "plugin", b.name, "error", err)
		return
	}
	logger.Infow("Mailed ROOT that a plugin did not build", "plugin", b.name, "message_id", messageID, "attestation", attestationID)
}
