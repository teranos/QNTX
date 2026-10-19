package server

// What the node's GitHub (github.go) asks of the node, and the node's side of
// what GitHub says: a build landed, a push moves a plugin's source.

import (
	"context"
	"net/http"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/sacred"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap"
)

var _ gitHubHost = (*QNTXServer)(nil)

// gh is the node's GitHub, made the first time it is asked for.
func (s *QNTXServer) gh() *gitHub {
	s.githubOnce.Do(func() { s.github = &gitHub{host: s} })
	return s.github
}

// gitHubService is the node's GitHubService.
func (s *QNTXServer) gitHubService() *services.GitHubServer { return s.gh().gitHubService() }

func (s *QNTXServer) githubSignum() sigil.Signum { return s.gh().signum() }

func (s *QNTXServer) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	s.gh().HandleGitHubWebhook(w, r)
}

func (s *QNTXServer) HandleGitHubCredential(w http.ResponseWriter, r *http.Request) {
	s.gh().HandleGitHubCredential(w, r)
}

func (s *QNTXServer) StartRunner() { s.gh().StartRunner() }

func (s *QNTXServer) CatchUpDeliveries() { s.gh().CatchUpDeliveries() }

func (s *QNTXServer) log() *zap.SugaredLogger { return s.logger }

func (s *QNTXServer) login() *auth.Handler { return s.authHandler }

func (s *QNTXServer) dbFile() string { return s.dbPath }

func (s *QNTXServer) serves(namespace string) (string, error) { return s.held.Serves(namespace) }

// fillBuilds adds what the node built and took of its plugins to GitHub's status.
func (s *QNTXServer) fillBuilds(status *protocol.GitHubStatus) {
	status.Builds = map[string]*protocol.PluginBuild{}
	status.Artifacts = takenBuilds(s.artifacts.all())
	status.ArtifactFaults = s.artifacts.faulted()
	for name, build := range s.builds.all() {
		status.Builds[name] = &protocol.PluginBuild{Revs: build.Revs, Digest: build.Digest,
			At: build.At.Format(time.RFC3339Nano), Changed: build.Changed, Error: build.Error}
	}
}

// lifetime is the server's own context, or one that never ends in a server
// that was not started.
func (s *QNTXServer) lifetime() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// buildLanded runs a plugin's new build, when the plugin is enabled.
func (s *QNTXServer) buildLanded(name string) {
	// A build the runner delivered, or one started again because what it loads
	// from was collected, is a deploy of the archive installed. One installed
	// with no digest recorded names no rev, and begins none.
	if s.deploys.inFlight(name) {
		s.deploys.stage(name, deployRestarting)
	} else if digest, recorded := grpcplugin.InstalledDigest(name); recorded {
		s.deploys.begin(pluginDeploy{plugin: name, rev: digest, stage: deployRestarting})
	}
	record, found, err := s.pluginRecords().Plugin(name)
	if err != nil {
		s.deploys.fail(name, err)
		s.logger.Errorw("A build landed and the plugin records were not read", "plugin", name, "error", err)
		return
	}
	if !found || !record.Enabled {
		s.deploys.stage(name, deployDisabled)
		s.logger.Infow("A build landed for a plugin that is not enabled; it runs once enabled", "plugin", name, "known", found)
		return
	}
	pm, err := s.getPluginManager()
	if err != nil {
		s.deploys.fail(name, err)
		s.logger.Errorw("A build landed and there is no plugin manager to run it", "plugin", name, "error", err)
		return
	}
	s.pluginMuxes.Delete(name)
	s.pluginMuxInit.Delete(name)
	if err := pm.RestartPlugin(s.lifetime(), name, pluginSearchPaths(), s.pluginRegistry, s.services); err != nil {
		s.deploys.fail(name, err)
		s.logger.Errorw("A build landed and the plugin did not start from it", "plugin", name, "error", err)
		return
	}
	s.deploys.stage(name, deployLive)
	s.BroadcastPluginHealth(name, true, "running", "Started from the build the runner delivered")
}

// buildsMovedBy starts the build of every enabled plugin push moves, and names them.
func (s *QNTXServer) buildsMovedBy(push forge.Push) []string {
	logger := s.logger.Named("build")
	records, err := s.pluginRecords().Plugins()
	if err != nil {
		logger.Errorw("A push arrived and the plugin records were not read", "repo", push.Repo.String(), "error", err)
		return []string{}
	}
	names := []string{}
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		b, built, err := buildOf(record)
		if err != nil {
			logger.Errorw("A plugin's build is not one QNTX can run", "plugin", record.Name, "error", err)
			continue
		}
		if !built || !pushMoves(push, b) {
			continue
		}
		names = append(names, b.name)
		logger.Infow("A push moves a plugin's source", "plugin", b.name, "repo", push.Repo.String(), "ref", push.Ref, "after", push.After)
		// Built once GitHub's checks on the commit pushed have passed.
		s.deploys.begin(pluginDeploy{plugin: b.name, rev: push.After, repo: push.Repo.String(), ref: push.Ref, after: push.After, pushed: true, stage: deployChecking})
		sacred.Go("plugin.build."+b.name, func() {
			s.awaitChecks(s.lifetime(), b, push.Repo, push.After, logger)
		})
	}
	return names
}
