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

// buildSource is one repository a build takes from, at a branch; Path names a
// single file of it, and is empty for the source that is built.
type buildSource struct {
	Owner, Repo, Branch, Path string
}

func (b buildSource) String() string {
	s := b.Owner + "/" + b.Repo + "@" + b.Branch
	if b.Path != "" {
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
	return buildSource{Owner: owner, Repo: name, Branch: branch, Path: path}, nil
}

// pluginBuild is how one plugin is built, as its record says.
type pluginBuild struct {
	name      string
	core      buildSource
	inputs    []buildSource
	inputsEnv string
	packages  []string
	command   string
	output    string
}

// buildOf is the build a record names. False is a plugin QNTX does not build.
func buildOf(record grpcplugin.PluginRecord) (pluginBuild, bool, error) {
	core, ok := record.Config[buildCore]
	if !ok || strings.TrimSpace(core) == "" {
		return pluginBuild{}, false, nil
	}
	b := pluginBuild{
		name:      record.Name,
		inputsEnv: record.Config[buildInputsEnv],
		packages:  strings.Fields(record.Config[buildPackages]),
		command:   strings.TrimSpace(record.Config[buildCommand]),
		output:    strings.TrimSpace(record.Config[buildOutput]),
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
		b.inputs = append(b.inputs, source)
	}
	switch {
	case b.command == "":
		return pluginBuild{}, false, errors.Newf("plugin %s names no %s", record.Name, buildCommand)
	case b.output == "":
		return pluginBuild{}, false, errors.Newf("plugin %s names no %s", record.Name, buildOutput)
	case len(b.inputs) > 0 && b.inputsEnv == "":
		return pluginBuild{}, false, errors.Newf("plugin %s takes %s and names no %s", record.Name, buildInputs, buildInputsEnv)
	}
	return b, true, nil
}

// enabledBuilds is each enabled plugin QNTX builds, and why any record's build
// is not one QNTX can run.
func enabledBuilds(records []grpcplugin.PluginRecord) ([]pluginBuild, []error) {
	var builds []pluginBuild
	var refused []error
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		b, built, err := buildOf(record)
		if err != nil {
			refused = append(refused, err)
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
// plugin with no binary, or one whose binary QNTX did not build.
func installedRevs(name string) []string {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(filepath.Join(dir, grpcplugin.PluginBinaryName(name))); err != nil || info.IsDir() {
		return nil
	}
	held, err := os.ReadFile(filepath.Join(dir, builtRevsFile))
	if err != nil {
		return nil
	}
	return strings.Fields(string(held))
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

// keepFailed writes which build failed beside the install; a built one clears it.
func keepFailed(name, key string) error {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return err
	}
	if key == "" {
		if err := os.Remove(filepath.Join(dir, failedBuildFile)); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "the failed build of %s was not cleared", name)
		}
		return nil
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
	for _, err := range refused {
		logger.Errorw("A plugin's build is not one QNTX can run", "error", err)
	}
	for _, b := range builds {
		if revs := installedRevs(b.name); revs != nil {
			s.builds.set(b.name, PluginBuildState{Revs: revs, At: time.Now()})
		}
		sacred.Go("plugin.build."+b.name, func() {
			s.building.Lock()
			defer s.building.Unlock()
			s.buildIfMoved(s.lifetime(), b, logger)
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

// pluginBuilds is what the builds know between rounds.
type pluginBuilds struct {
	mu    sync.Mutex
	state map[string]PluginBuildState
}

func (p *pluginBuilds) set(name string, state PluginBuildState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == nil {
		p.state = map[string]PluginBuildState{}
	}
	p.state[name] = state
}

func (p *pluginBuilds) get(name string) (PluginBuildState, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.state[name]
	return state, ok
}

func (p *pluginBuilds) all() map[string]PluginBuildState {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]PluginBuildState{}
	for name, state := range p.state {
		out[name] = state
	}
	return out
}

// buildIfMoved asks GitHub where every source of b is now, and builds when
// that is not what was last built.
func (s *QNTXServer) buildIfMoved(ctx context.Context, b pluginBuild, logger *zap.SugaredLogger) {
	var revs []string
	for _, source := range b.sources() {
		rev, err := s.buildRev(ctx, source)
		if err != nil {
			logger.Warnw("A source's rev was not read, so the plugin is not built", "plugin", b.name, "source", source.String(), "error", err)
			s.builds.set(b.name, PluginBuildState{At: time.Now(), Error: err.Error()})
			s.mailBuildFailure(ctx, b, nil, err, logger)
			return
		}
		revs = append(revs, rev)
	}
	if held, ok := s.builds.get(b.name); ok && held.Error == "" && strings.Join(held.Revs, " ") == strings.Join(revs, " ") {
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
	changed, digest, err := s.buildPlugin(ctx, b, revs)
	state.Changed, state.Digest = changed, digest
	if err != nil {
		state.Error = err.Error()
		logger.Warnw("A plugin was not built", "plugin", b.name, "revs", revs, "error", err)
		s.builds.set(b.name, state)
		s.mailBuildFailure(ctx, b, revs, err, logger)
		if err := keepFailed(b.name, key); err != nil {
			logger.Errorw("A failed build was not kept, so the next start builds and mails it again", "plugin", b.name, "error", err)
		}
		return
	}
	s.builds.set(b.name, state)
	if err := keepBuiltRevs(b.name, revs); err != nil {
		logger.Errorw("A plugin built and what it was built from was not kept, so the next start builds it again", "plugin", b.name, "error", err)
	}
	if err := keepFailed(b.name, ""); err != nil {
		logger.Errorw("A plugin built and its earlier failure was not cleared", "plugin", b.name, "error", err)
	}
	if changed {
		s.buildLanded(b.name)
	}
}

// buildRev is the commit source's branch points at now, or the last commit
// that touched its file.
func (s *QNTXServer) buildRev(ctx context.Context, source buildSource) (string, error) {
	said, err := s.gitHubService().ListCommits(ctx, &protocol.GitHubListCommitsRequest{
		Owner:   source.Owner,
		Repo:    source.Repo,
		Sha:     source.Branch,
		Path:    source.Path,
		PerPage: 1,
	})
	if err != nil {
		return "", err
	}
	if !said.Success {
		return "", errors.New(said.Error)
	}
	if len(said.Items) == 0 || said.Items[0].Sha == "" {
		return "", errors.Newf("GitHub has no commit for %s", source.String())
	}
	return said.Items[0].Sha, nil
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
func buildEnv(work, inputsEnv string, files []string) []string {
	env := []string{
		"PATH=" + nixBin + ":" + os.Getenv("PATH"),
		"TMPDIR=" + filepath.Join(work, "tmp"),
	}
	if inputsEnv != "" {
		env = append(env, inputsEnv+"="+strings.Join(files, " "))
	}
	return env
}

// gentle runs a build at the lowest CPU priority, so the node it builds on
// keeps answering.
func gentle(name string, args []string) (string, []string) {
	return "nice", append([]string{"-n", "19", name}, args...)
}

// buildPlugin builds b from revs, packages what it built, and installs it.
func (s *QNTXServer) buildPlugin(ctx context.Context, b pluginBuild, revs []string) (bool, string, error) {
	builds, err := buildsDir()
	if err != nil {
		return false, "", err
	}
	work, err := os.MkdirTemp(builds, b.name+"-")
	if err != nil {
		return false, "", errors.Wrapf(err, "failed to make a directory in %s to build in", builds)
	}
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return false, "", errors.Wrapf(err, "failed to make the build's temp directory in %s", work)
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			s.logger.Warnw("A build's directory was not removed", "path", work, "error", err)
		}
	}()

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
	if _, err := runBuild(ctx, work, nil, "tar", "-xzf", tarball, "--strip-components=1", "-C", src); err != nil {
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
	if _, err := runBuild(ctx, src, buildEnv(work, b.inputsEnv, files), nice, niceArgs...); err != nil {
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
	body, err := s.gitHubService().Tarball(ctx, "", core.Owner, core.Repo, rev)
	if err != nil {
		return errors.Wrapf(err, "failed to fetch %s at %s", core.String(), rev)
	}
	defer func() { sqlclose.Log(body.Close(), s.logger, "the archive of "+core.String()) }()
	file, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "failed to make %s", path)
	}
	if _, err := io.Copy(file, body); err != nil {
		sqlclose.Log(file.Close(), s.logger, path)
		return errors.Wrapf(err, "failed to write %s at %s to %s", core.String(), rev, path)
	}
	if err := file.Close(); err != nil {
		return errors.Wrapf(err, "failed to finish writing %s", path)
	}
	return nil
}

// fetchInput writes one input file at rev into work, and says where.
func (s *QNTXServer) fetchInput(ctx context.Context, in buildSource, rev, work string) (string, error) {
	said, err := s.gitHubService().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
		Owner: in.Owner,
		Repo:  in.Repo,
		Path:  in.Path,
		Ref:   rev,
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

// runBuild runs one step of a build in dir and hands back what it printed. A
// step that fails says what it printed last.
func runBuild(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		said := errOut.String()
		if len(said) > 2000 {
			said = said[len(said)-2000:]
		}
		return "", errors.Wrapf(err, "%s %s: %s", name, strings.Join(args, " "), said)
	}
	return out.String(), nil
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
