package server

// QNTX builds a plugin itself (ADR-045): from the sources its record names, as
// their branches are now, with the node's nix, and runs what it built.
// "it is qntx that actually owns this"

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
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
			logger.Errorw("A source's rev was not read, so the plugin is not built", "plugin", b.name, "source", source.String(), "error", err)
			s.builds.set(b.name, PluginBuildState{At: time.Now(), Error: err.Error()})
			return
		}
		revs = append(revs, rev)
	}
	if held, ok := s.builds.get(b.name); ok && held.Error == "" && strings.Join(held.Revs, " ") == strings.Join(revs, " ") {
		return
	}

	logger.Infow("Building a plugin whose sources moved", "plugin", b.name, "revs", revs)
	state := PluginBuildState{Revs: revs, At: time.Now()}
	changed, digest, err := s.buildPlugin(ctx, b, revs)
	state.Changed, state.Digest = changed, digest
	if err != nil {
		state.Error = err.Error()
		logger.Errorw("A plugin was not built", "plugin", b.name, "revs", revs, "error", err)
		s.builds.set(b.name, state)
		return
	}
	s.builds.set(b.name, state)
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

// buildPlugin builds b from revs, packages what it built, and installs it.
func (s *QNTXServer) buildPlugin(ctx context.Context, b pluginBuild, revs []string) (bool, string, error) {
	work, err := os.MkdirTemp("", "qntx-build-"+b.name+"-")
	if err != nil {
		return false, "", errors.Wrap(err, "failed to make a directory to build in")
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			s.logger.Warnw("A build's directory was not removed", "path", work, "error", err)
		}
	}()

	src := filepath.Join(work, "src")
	fetched, err := runBuild(ctx, work, nil, filepath.Join(nixBin, "nix"), "eval", "--raw", "--impure", "--expr",
		fmt.Sprintf(`builtins.fetchTarball "https://github.com/%s/%s/archive/%s.tar.gz"`, b.core.Owner, b.core.Repo, revs[0]))
	if err != nil {
		return false, "", errors.Wrapf(err, "failed to fetch %s at %s", b.core.String(), revs[0])
	}
	if _, err := runBuild(ctx, work, nil, "cp", "-R", strings.TrimSpace(fetched), src); err != nil {
		return false, "", err
	}
	if _, err := runBuild(ctx, work, nil, "chmod", "-R", "u+w", src); err != nil {
		return false, "", err
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
	env := []string{"PATH=" + nixBin + ":" + os.Getenv("PATH")}
	if b.inputsEnv != "" {
		env = append(env, b.inputsEnv+"="+strings.Join(files, " "))
	}
	if _, err := runBuild(ctx, src, env, filepath.Join(nixBin, "nix"), args...); err != nil {
		return false, "", errors.Wrapf(err, "the build of %s at %s failed", b.name, revs[0])
	}

	archive, err := grpcplugin.PackageBuild(b.name, filepath.Join(src, b.output))
	if err != nil {
		return false, "", err
	}
	return grpcplugin.InstallBuild(b.name, archive, s.logger.Named("build"))
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
