package server

// The node's GitHub (ADR-043): the GitHubService it spends, the switch that
// turns GitHub off on the node entirely, and the Actions runner whose builds
// it runs. The GitHub element is what reads and sets all three.

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

const githubPath = "/api/github"

// gitHubCredentials hands GitHubService the GITHUB token a namespace spends.
type gitHubCredentials struct{ s *QNTXServer }

func (c gitHubCredentials) Token(ctx context.Context, namespace string) (string, string, error) {
	if c.s.authHandler == nil {
		return "", "", errors.Newf("this node has no login, so namespace %q has no GitHub", namespace)
	}
	return c.s.authHandler.GitHubToken(ctx, namespace)
}

// AppToken is the JWT the App signs, for what GitHub takes only from the App.
func (c gitHubCredentials) AppToken() (string, error) {
	if c.s.authHandler == nil {
		return "", errors.New("this node has no login, so it holds no GitHub App")
	}
	return c.s.authHandler.GitHubAppToken()
}

// gitHubService is the node's GitHubService, made the first time it is asked for.
func (s *QNTXServer) gitHubService() *services.GitHubServer {
	s.githubOnce.Do(func() {
		s.github = services.NewGitHubServer(gitHubCredentials{s: s}, func() bool {
			settings, err := s.nodeRecords().GitHub()
			if err != nil {
				s.logger.Errorw("The node's GitHub settings were not read, so GitHub answers nothing", "error", err)
				return false
			}
			return settings.Enabled
		}, s.logger.Named("github"))
	})
	return s.github
}

// webhookURL is the URL to paste into the App's webhook settings.
func (s *QNTXServer) webhookURL(path string) string {
	if s.authHandler == nil {
		return path
	}
	return s.authHandler.PublicOrigin() + path
}

func (s *QNTXServer) githubSignum() sigil.Signum {
	enabled := &protocol.Param{Name: "enabled", Required: true, Says: "true or false."}
	var follows []*protocol.Follows
	if f, err := githubFollowGitHub(); err != nil {
		if s.logger != nil {
			s.logger.Errorw("GitHubService's messages were not read against GitHub's description, so parity cannot hold github", "error", err)
		}
	} else {
		follows = append(follows, f)
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:    "github",
			Follows: follows,
			Sigils: []*protocol.Sigil{
				{
					Name:   "status",
					Does:   "The node's GitHub: whether it is on, each namespace that has it with whether its token works and where it came from, and the Actions runner.",
					Answer: "protocol.GitHubStatus",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: githubPath},
				},
				{
					Name:   "webhook",
					Does:   "Generate the App's webhook secret, replacing the one before, and open the webhook's path. It is shown this once, to set in the App's settings.",
					Answer: "protocol.GitHubWebhook",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/webhook"},
				},
				{
					Name:   "webhook_path",
					Does:   "Set where the App's webhook URL points on this node, under " + githubWebhookPrefix + ". The secret stays as it is.",
					Takes:  []*protocol.Param{{Name: "path", Required: true, Says: "A path under " + githubWebhookPrefix + "."}},
					Answer: "protocol.GitHubWebhookPath",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/webhook/path"},
				},
				{
					Name:   "node",
					Does:   "Turn GitHub on or off for the node entirely.",
					Takes:  []*protocol.Param{enabled},
					Answer: "protocol.GitHubNode",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/node"},
				},
				{
					Name: "runner",
					Does: "Set where the Actions runner is and turn taking its plugin builds on or off. Refused when turned on where there is no runner.",
					Takes: []*protocol.Param{
						{Name: "path", Required: true, Says: "The runner's directory, the one holding .runner."},
						enabled,
					},
					Answer: "protocol.GitHubRunnerSet",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/runner"},
				},
				githubAskSigils()[0],
				githubAskSigils()[1],
			},
		},
		Answers: map[string]sigil.Answer{
			"status":       s.githubStatus,
			"node":         s.githubNode,
			"runner":       s.githubRunner,
			"webhook":      s.githubWebhook,
			"webhook_path": s.githubWebhookPath,
			"ask":          s.githubAsk,
			"operations":   s.githubOperations,
		},
	}
}

func (s *QNTXServer) githubStatus(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	status := &protocol.GitHubStatus{
		Enabled:    settings.Enabled,
		Namespaces: []*protocol.GitHubNamespace{},
		Runner:     s.runnerStatus(settings),
		Builds:     map[string]*protocol.PluginBuild{},
	}
	for name, build := range s.builds.all() {
		status.Builds[name] = &protocol.PluginBuild{Revs: build.Revs, Digest: build.Digest,
			At: build.At.Format(time.RFC3339Nano), Changed: build.Changed, Error: build.Error}
	}
	_, status.Webhook = s.gitHubWebhook()
	status.WebhookPath, status.WebhookUrl = settings.WebhookPath, s.webhookURL(settings.WebhookPath)
	if s.authHandler == nil {
		return status, nil
	}
	keeper, err := s.authHandler.GitHubKeeper()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	held, err := keeper.GitHubTokens()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, token := range held {
		namespace := strings.Join(token.Namespaces, ",")
		row := &protocol.GitHubNamespace{
			Namespace: namespace,
			Source:    token.GitHub.Source,
			Login:     token.GitHub.Login,
			MintedBy:  token.MintedBy,
			Revoked:   token.RevokedAt != nil,
		}
		// Whether the token works is asked of GitHub, not remembered.
		said, err := s.gitHubService().RateLimit(ctx, &protocol.GitHubRateLimitRequest{Namespace: namespace})
		switch {
		case err != nil:
			row.AuthError = err.Error()
		case !said.Success:
			row.AuthError = said.Error
		default:
			row.AuthOk = true
			if core, ok := said.Resources["core"]; ok {
				row.Rate = &protocol.GitHubCoreRate{Limit: uint32(core.Limit), Remaining: uint32(core.Remaining),
					Reset_: time.Unix(core.Reset_, 0).UTC().Format(time.RFC3339Nano)}
			}
		}
		status.Namespaces = append(status.Namespaces, row)
	}
	slices.SortFunc(status.Namespaces, func(a, b *protocol.GitHubNamespace) int {
		return strings.Compare(a.GetNamespace(), b.GetNamespace())
	})
	return status, nil
}

func (s *QNTXServer) githubNode(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	on, err := strconv.ParseBool(sent["enabled"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "enabled", Says: "enabled is true or false, and this said " + sent["enabled"]}
	}
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.Enabled = on
	if err := s.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubNode{Enabled: on}, nil
}

func (s *QNTXServer) githubRunner(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	on, err := strconv.ParseBool(sent["enabled"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "enabled", Says: "enabled is true or false, and this said " + sent["enabled"]}
	}
	path := strings.TrimSpace(sent["path"])
	if path == "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: "a runner has a path"}
	}
	if on {
		if _, err := grpcplugin.OpenRunner(path); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: err.Error()}
		}
	}
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.RunnerPath, settings.RunnerEnabled = path, on
	if err := s.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.StartRunner()
	return &protocol.GitHubRunnerSet{Runner: s.runnerStatus(settings)}, nil
}

func (s *QNTXServer) githubWebhook(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no login, so it keeps no webhook secret"}
	}
	secret, err := s.authHandler.NewGitHubWebhook(actorOf(ctx))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubWebhook{Secret: secret, Url: s.webhookURL(settings.WebhookPath)}, nil
}

func (s *QNTXServer) githubWebhookPath(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	path := strings.TrimSpace(sent["path"])
	if err := webhookPath(path); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: err.Error()}
	}
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.WebhookPath = path
	if err := s.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubWebhookPath{Url: s.webhookURL(path)}, nil
}

// runnerStatus is the runner as the Actions section shows it.
func (s *QNTXServer) runnerStatus(settings GitHubSettings) *protocol.GitHubRunner {
	shown := &protocol.GitHubRunner{Path: settings.RunnerPath, Enabled: settings.RunnerEnabled}
	if !settings.RunnerEnabled {
		return shown
	}
	s.runnerMu.Lock()
	runner, failed := s.runner, s.runnerErr
	s.runnerMu.Unlock()
	if runner == nil {
		shown.Error = failed
		if shown.Error == "" {
			shown.Error = "the runner is on and not watched yet"
		}
		return shown
	}
	shown.Stats = runnerStats(runner.Stats())
	return shown
}

// runnerStats is the runner's stats as the Actions section is given them.
func runnerStats(stats grpcplugin.RunnerStats) *protocol.RunnerStats {
	jobs := make([]string, 0, len(stats.Jobs))
	for _, at := range stats.Jobs {
		jobs = append(jobs, at.Format(time.RFC3339Nano))
	}
	taken := make([]*protocol.TakenBuild, 0, len(stats.Taken))
	for _, build := range stats.Taken {
		taken = append(taken, &protocol.TakenBuild{Plugin: build.Plugin, Archive: build.Archive, Digest: build.Digest,
			At: build.At.Format(time.RFC3339Nano), Changed: build.Changed, Older: build.Older})
	}
	return &protocol.RunnerStats{Path: stats.Path, Name: stats.Name, GithubUrl: stats.GitHubURL,
		Workspaces: stats.Workspaces, Jobs: jobs, Taken: taken}
}

// StartRunner watches the runner the node's settings name, or stops watching
// when they name none. Called at start and whenever the settings change.
func (s *QNTXServer) StartRunner() {
	s.stopRunner()
	settings, err := s.nodeRecords().GitHub()
	if err != nil {
		s.logger.Errorw("The node's GitHub settings were not read, so no runner is watched", "error", err)
		return
	}
	if !settings.RunnerEnabled {
		return
	}
	runner, err := grpcplugin.OpenRunner(settings.RunnerPath)
	if err != nil {
		s.runnerFailed(err)
		return
	}
	ctx, cancel := context.WithCancel(s.lifetime())
	if err := runner.Watch(ctx, s.buildLanded, s.logger.Named("runner")); err != nil {
		cancel()
		s.runnerFailed(err)
		return
	}
	s.runnerMu.Lock()
	s.runner, s.runnerStop, s.runnerErr = runner, cancel, ""
	s.runnerMu.Unlock()
}

func (s *QNTXServer) runnerFailed(err error) {
	s.logger.Errorw("The runner is on and not watched", "error", err)
	s.runnerMu.Lock()
	s.runnerErr = err.Error()
	s.runnerMu.Unlock()
}

func (s *QNTXServer) stopRunner() {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	if s.runnerStop != nil {
		s.runnerStop()
	}
	s.runner, s.runnerStop, s.runnerErr = nil, nil, ""
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
	record, found, err := s.pluginRecords().Plugin(name)
	if err != nil {
		s.logger.Errorw("A build landed and the plugin records were not read", "plugin", name, "error", err)
		return
	}
	if !found || !record.Enabled {
		s.logger.Infow("A build landed for a plugin that is not enabled; it runs once enabled", "plugin", name, "known", found)
		return
	}
	pm := s.getPluginManager()
	if pm == nil || s.pluginRegistry == nil {
		s.logger.Errorw("A build landed and there is no plugin manager to run it", "plugin", name)
		return
	}
	s.pluginMuxes.Delete(name)
	s.pluginMuxInit.Delete(name)
	if err := pm.RestartPlugin(s.lifetime(), name, pluginSearchPaths(), s.pluginRegistry, s.services); err != nil {
		s.logger.Errorw("A build landed and the plugin did not start from it", "plugin", name, "error", err)
		return
	}
	s.BroadcastPluginHealth(name, true, "running", "Started from the build the runner delivered")
}
