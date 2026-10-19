package server

// A plugin built by a runner on another machine reaches the node as an
// artifact of a successful workflow run in its record's repository (ADR-043).

// "do you want plugin builds downloaded from GitHub?"
// "yes"

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// TODO(forge): GitHub's workflow_run payload. A forge's adapter reads its own pipeline-finished event into a forge event.
// gitHubWorkflowRun is the part of a workflow_run event a build reads.
type gitHubWorkflowRun struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		ID         int64     `json:"id"`
		HeadSha    string    `json:"head_sha"`
		HeadBranch string    `json:"head_branch"`
		Conclusion string    `json:"conclusion"`
		HTMLURL    string    `json:"html_url"`
		CreatedAt  time.Time `json:"created_at"`
	} `json:"workflow_run"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// pluginOfRun is an enabled plugin whose record names the repository a run is in.
type pluginOfRun struct {
	name   string
	source gitHubSource
}

// pluginsOfRun is each enabled plugin whose record names repo, as GitHub
// names it, and branch: the branch its URL names, or the repository's default
// branch when it names none. A record naming no repository on GitHub is of no run.
func pluginsOfRun(records []grpcplugin.PluginRecord, repo, branch, defaultBranch string) []pluginOfRun {
	var of []pluginOfRun
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		// TODO(forge): A plugin's repository is read for any forge the node reaches (see plugin_check.go), and compared as a forge.Repo, not by owner/repo against the run's full_name.
		source, err := readGitHubSource(record.Repo)
		if err != nil || !strings.EqualFold(source.Owner+"/"+source.Repo, repo) {
			continue
		}
		named := defaultBranch
		if source.RefNamed {
			named = source.Ref
		}
		if branch != named {
			continue
		}
		of = append(of, pluginOfRun{name: record.Name, source: source})
	}
	return of
}

// buildsOfRun takes, out of a successful run's artifacts, the build of every
// enabled plugin whose repository it ran in, and names them. Records not read
// fail the delivery, so GitHub keeps it as one to redeliver.
func (s *QNTXServer) buildsOfRun(run gitHubWorkflowRun) ([]string, error) {
	names := []string{}
	if run.Action != "completed" || run.WorkflowRun.Conclusion != "success" {
		return names, nil
	}
	logger := s.logger.Named("artifact")
	records, err := s.pluginRecords().Plugins()
	if err != nil {
		return nil, errors.Wrapf(err, "%s succeeded and the plugin records were not read", run.WorkflowRun.HTMLURL)
	}
	for _, of := range pluginsOfRun(records, run.Repository.FullName, run.WorkflowRun.HeadBranch, run.Repository.DefaultBranch) {
		names = append(names, of.name)
		logger.Infow("A workflow run succeeded in a plugin's repository", "plugin", of.name, "run", run.WorkflowRun.HTMLURL, "head_sha", run.WorkflowRun.HeadSha)
		// The take runs on its own, with no caller to hand an error to: what it
		// could not do is said where the builds are shown.
		sacred.Go("plugin.artifact."+of.name, func() {
			if err := s.takeRun(s.lifetime(), of.name, of.source, run); err != nil {
				s.artifacts.fault(of.name + " from " + run.WorkflowRun.HTMLURL + " was not installed: " + err.Error())
			}
		})
	}
	return names, nil
}

// takeRun downloads each of run's artifacts that is name's build for this
// platform, and installs it unless the installed build is of a newer run.
func (s *QNTXServer) takeRun(ctx context.Context, name string, source gitHubSource, run gitHubWorkflowRun) error {
	logger := s.logger.Named("artifact")
	// TODO(forge): A pipeline run's artifacts are a Forge method (Reader), not GitHubService's ListWorkflowRunArtifacts.
	listed, err := s.gitHubService().ListWorkflowRunArtifacts(services.AsInstallation(ctx), &protocol.GitHubListWorkflowRunArtifactsRequest{
		Owner: source.Owner, Repo: source.Repo, RunId: run.WorkflowRun.ID, PerPage: 100})
	if err != nil {
		return errors.Wrapf(err, "the artifacts of %s were not listed", run.WorkflowRun.HTMLURL)
	}
	if !listed.Success {
		return errors.Newf("the artifacts of %s were not listed: %s", run.WorkflowRun.HTMLURL, listed.Error)
	}
	for _, artifact := range listed.Artifacts {
		if plugin, isBuild := grpcplugin.BuildPlugin(artifact.Name); artifact.Expired || !isBuild || plugin != name {
			continue
		}
		zipped, err := s.downloadArtifact(ctx, source, artifact)
		if err != nil {
			return err
		}
		archive, digest, err := grpcplugin.ArtifactBuild(name, zipped)
		if err != nil {
			return errors.Wrapf(err, "artifact %s of %s", artifact.Name, run.WorkflowRun.HTMLURL)
		}
		taken, err := s.installRun(name, artifact.Name, archive, digest, run, logger)
		if err != nil {
			return err
		}
		if taken.Changed {
			s.buildLanded(name)
		}
	}
	return nil
}

// downloadArtifact is what one artifact holds, as GitHub zips it.
func (s *QNTXServer) downloadArtifact(ctx context.Context, source gitHubSource, artifact *protocol.GitHubActionsArtifactsArtifact) ([]byte, error) {
	// TODO(forge): Downloading a pipeline run's artifact is a Forge method (Reader), not GitHubService's Artifact.
	body, err := s.gitHubService().Artifact(ctx, source.Owner, source.Repo, artifact.Id)
	if err != nil {
		return nil, errors.Wrapf(err, "artifact %s of %s/%s was not downloaded", artifact.Name, source.Owner, source.Repo)
	}
	zipped, err := io.ReadAll(body)
	if err != nil {
		err = errors.Wrapf(err, "artifact %s of %s/%s stopped downloading after %d bytes", artifact.Name, source.Owner, source.Repo, len(zipped))
	}
	return zipped, sqlclose.With(err, body.Close(), "the download of artifact "+artifact.Name)
}

// installRun installs archive as name's build of run, unless the installed
// build is of a newer run, and keeps which run it was.
func (s *QNTXServer) installRun(name, artifact string, archive []byte, digest string, run gitHubWorkflowRun, logger *zap.SugaredLogger) (grpcplugin.TakenBuild, error) {
	s.artifacts.mu.Lock()
	defer s.artifacts.mu.Unlock()
	taken := grpcplugin.TakenBuild{Plugin: name, Archive: artifact, Digest: digest, At: time.Now(), Run: run.WorkflowRun.HTMLURL}
	if installed, ok := installedRun(name); ok && run.WorkflowRun.CreatedAt.Before(installed) {
		taken.Older = true
		logger.Infow("A workflow run is older than the one whose build is installed; its build is not installed",
			"plugin", name, "run", run.WorkflowRun.HTMLURL, "created_at", run.WorkflowRun.CreatedAt, "installed_run_created_at", installed)
		s.artifacts.record(taken)
		return taken, nil
	}
	changed, installed, err := grpcplugin.InstallBuild(name, archive, logger)
	if err != nil {
		return grpcplugin.TakenBuild{}, errors.Wrapf(err, "artifact %s of %s", artifact, run.WorkflowRun.HTMLURL)
	}
	if err := keepBuiltRun(name, run.WorkflowRun.CreatedAt); err != nil {
		return grpcplugin.TakenBuild{}, err
	}
	taken.Changed, taken.Digest = changed, installed
	s.artifacts.record(taken)
	return taken, nil
}

// builtRunFile is beside a build installed from a workflow run's artifact:
// when that run was created.
const builtRunFile = "built-run"

// installedRun is when the run name's installed build came from was created.
// False is a plugin whose build came from no run, or whose run was not kept.
func installedRun(name string) (time.Time, bool) {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return time.Time{}, false
	}
	held, err := os.ReadFile(filepath.Join(dir, builtRunFile))
	if err != nil {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(held)))
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// keepBuiltRun writes when the run name's installed build came from was created.
func keepBuiltRun(name string, createdAt time.Time) error {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, builtRunFile), []byte(createdAt.Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return errors.Wrapf(err, "the run %s's build came from was not kept", name)
	}
	return nil
}

// artifactTakes is every build the node took from a workflow run's artifact,
// one row per build. Its lock makes one run's install at a time.
type artifactTakes struct {
	mu    sync.Mutex
	taken []grpcplugin.TakenBuild
	// faults is what of a run's build was not installed, and why.
	faults []string
}

// fault keeps what of a run's build was not installed, for the status to show.
func (a *artifactTakes) fault(what string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.faults = append(a.faults, time.Now().Format(time.RFC3339)+" "+what)
}

// faulted is every fault kept, oldest first.
func (a *artifactTakes) faulted() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.faults...)
}

// record keeps one row per build of a run: a build seen again replaces its
// row, and a row that installed it stays marked as having changed the plugin.
// Held under mu.
func (a *artifactTakes) record(taken grpcplugin.TakenBuild) {
	for i, held := range a.taken {
		if held.Plugin == taken.Plugin && held.Digest == taken.Digest && held.Run == taken.Run {
			taken.Changed = taken.Changed || held.Changed
			a.taken[i] = taken
			return
		}
	}
	a.taken = append(a.taken, taken)
}

// all is every build taken, oldest first.
func (a *artifactTakes) all() []grpcplugin.TakenBuild {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]grpcplugin.TakenBuild{}, a.taken...)
}
