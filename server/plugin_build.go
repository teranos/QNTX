package server

// QNTX builds a plugin itself (ADR-045): from the sources its record names, as
// their branches are now, with the node's nix, and runs what it built.
// "it is qntx that actually owns this"

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
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

// nixBin is where the node's nix is: not on the service's PATH.
const nixBin = "/nix/var/nix/profiles/default/bin"

// buildSource is one repository a build takes from, at a branch. An input
// names a single file of it in Path; the source that is built names none.
type buildSource struct {
	Owner, Repo, Branch, Path string
	PathNamed                 bool
}

func (b buildSource) String() string {
	s := b.Owner + "/" + b.Repo + "@" + b.Branch
	if b.PathNamed {
		s += ":" + b.Path
	}
	return s
}

// parseBuildSource reads owner/repo@branch, with :path when a file is named.
func parseBuildSource(s string, withPath bool) (buildSource, error) {
	repo, branch, ok := strings.Cut(strings.TrimSpace(s), "@")
	if !ok || branch == "" {
		return buildSource{}, errors.Newf("%q names no branch: owner/repo@branch", s)
	}
	var path string
	if withPath {
		branch, path, ok = strings.Cut(branch, ":")
		if !ok || path == "" || branch == "" {
			return buildSource{}, errors.Newf("%q names no file: owner/repo@branch:path", s)
		}
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return buildSource{}, errors.Newf("%q names no repository: owner/repo", s)
	}
	return buildSource{Owner: owner, Repo: name, Branch: branch, Path: path, PathNamed: withPath}, nil
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
			s.mailBuildFailure(ctx, b, nil, err, logger)
			return
		}
		revs = append(revs, rev)
	}
	if s.builds.builtFrom(b.name, revs) {
		return
	}
	// A build that failed fails again at the same revs with the same recipe,
	// and ROOT was mailed the first time.
	key := b.failedKey(revs)
	if failedBefore(b.name, key) {
		logger.Infow("Not building again: this build failed before, and ROOT was told", "plugin", b.name, "revs", revs)
		s.builds.set(b.name, PluginBuildState{Revs: revs, At: time.Now(), Error: "this build failed before at these revs; a push or a change to its record builds it again"})
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
	// A failed build is said where the builds are shown, and mailed to ROOT.
	if err != nil {
		if keptErr := keepFailed(b.name, key); keptErr != nil {
			unkept = append(unkept, "and it was not kept as failed, so the next start builds and mails it again: "+keptErr.Error())
		}
		state.Error = strings.Join(append([]string{err.Error()}, unkept...), "; ")
		s.builds.set(b.name, state)
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
	if done.changed {
		s.buildLanded(b.name)
	}
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

// buildRev is the commit source's branch points at now, or the last commit
// that touched its file.
func (s *QNTXServer) buildRev(ctx context.Context, source buildSource) (string, error) {
	said, err := s.gitHubService().ListCommits(ctx, &protocol.GitHubListCommitsRequest{
		Namespace: auth.NamespaceSystem,
		Owner:     source.Owner,
		Repo:      source.Repo,
		Sha:       source.Branch,
		Path:      source.Path,
		PerPage:   1,
	})
	if err != nil {
		return "", err
	}
	if !said.Success {
		return "", errors.New(said.Error)
	}
	// PerPage is 1: the one commit GitHub hands back is where source is now.
	for _, commit := range said.Items {
		return commit.Sha, nil
	}
	return "", errors.Newf("GitHub has no commit for %s", source.String())
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

	archive, err := grpcplugin.PackageBuild(b.name, filepath.Join(src, b.output))
	if err != nil {
		return false, "", err
	}
	return grpcplugin.InstallBuild(b.name, archive, s.logger.Named("build"))
}

// fetchCore writes the core's archive at rev to path.
func (s *QNTXServer) fetchCore(ctx context.Context, core buildSource, rev, path string) error {
	body, err := s.gitHubService().Tarball(ctx, auth.NamespaceSystem, core.Owner, core.Repo, rev)
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
	said, err := s.gitHubService().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
		Namespace: auth.NamespaceSystem,
		Owner:     in.Owner,
		Repo:      in.Repo,
		Path:      in.Path,
		Ref:       rev,
	})
	if err != nil {
		return "", err
	}
	if !said.Success {
		return "", errors.Newf("%s: %s", in.String(), said.Error)
	}
	if said.Encoding != "base64" {
		return "", errors.Newf("%s came as %q, not base64", in.String(), said.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(said.Content, "\n", ""))
	if err != nil {
		return "", errors.Wrapf(err, "%s is not base64", in.String())
	}
	dir := filepath.Join(work, "inputs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s", dir)
	}
	file := filepath.Join(dir, in.Owner+"-"+in.Repo+"-"+filepath.Base(in.Path))
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
		Text:    text.String(),
		HTML:    "<pre>" + html.EscapeString(text.String()) + "</pre>",
	}
}

// mailBuildFailure mails ROOT that b did not build, and says so when it cannot.
func (s *QNTXServer) mailBuildFailure(ctx context.Context, b pluginBuild, revs []string, failed error, logger *zap.SugaredLogger) {
	if s.nodeMailer == nil || s.authHandler == nil {
		logger.Errorw("A plugin did not build and the node has no mail to say so with", "plugin", b.name)
		return
	}
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
