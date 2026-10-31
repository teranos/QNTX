package server

// The node's GitHub (ADR-043): the GitHubService it spends, the switch that
// turns GitHub off on the node entirely, and the Actions runner whose builds
// it runs. The GitHub element is what reads and sets all three.

import (
	"context"
	"github.com/teranos/QNTX/server/quote"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// TODO(forge): The route, the github signum and its sigils name the node's forge as GitHub. The core holds forges; what is GitHub's alone (the Actions runner, the App's webhook secret) is marked as such under it.
const githubPath = "/api/github"

// TODO(forge): The core holds Forges (s.forges()). This struct holds GitHub's service, webhook, follows, runner and deliveries: rename it when what is forge-neutral has moved out, and what is left is GitHub's.
// gitHub is the node's GitHub (ADR-043): the GitHubService, the runner, the
// App's webhook, the follows and the catch-up of deliveries. What it needs of
// the node is gitHubHost, and nothing else.
type gitHub struct {
	host gitHubHost

	// The GitHubService, made the first time it is asked for.
	serviceOnce sync.Once
	service     *services.GitHubServer
	// The runner whose plugin builds the node takes, while one is watched.
	runnerMu   sync.Mutex
	runner     *grpcplugin.Runner
	runnerStop context.CancelFunc
	runnerErr  string
}

// gitHubHost is everything GitHub asks of the node: its login, its records,
// its life, and what the node does when GitHub says a build landed, a
// workflow ran, a pull request moved or a push arrived.
type gitHubHost interface {
	log() *zap.SugaredLogger
	login() *auth.Handler
	nodeRecords() NodeRecords
	lifetime() context.Context
	dbFile() string
	serves(namespace string) (string, error)
	fillBuilds(*protocol.GitHubStatus)

	buildLanded(name string)
	buildsOfRun(gitHubWorkflowRun) ([]string, error)
	approvalsFromGitHub(ctx context.Context, event string, body []byte) ([]string, error)
	fixturesFromGitHub(ctx context.Context, event string, body []byte) ([]string, error)
	quotesFromGitHub(ctx context.Context, event string, body []byte) (*quote.Arrived, error)
	buildsMovedBy(forge.Push) []string
	vaultsMovedBy(forge.Push) []string
	cometsMovedBy(forge.Push) []string
}

// TODO(forge): Credentials per forge host (see GitHubKeeper).
// gitHubCredentials hands GitHubService the GITHUB token a namespace spends.
type gitHubCredentials struct{ host gitHubHost }

// "ROOT will determine what GitHub integration is active there, either system or have the namespace take responsibility"
func (c gitHubCredentials) Token(ctx context.Context, namespace string) (string, string, error) {
	if c.host.login() == nil {
		return "", "", errors.Newf("this node has no login, so namespace %q has no GitHub", namespace)
	}
	from, err := c.host.nodeRecords().GitHubFrom()
	if err != nil {
		return "", "", errors.Wrapf(err, "which GitHub namespace %q spends was not read", namespace)
	}
	if from[namespace] == GitHubFromSystem {
		return c.host.login().GitHubToken(ctx, auth.NamespaceSystem)
	}
	return c.host.login().GitHubToken(ctx, namespace)
}

// AppToken is the JWT the App signs, for what GitHub takes only from the App.
func (c gitHubCredentials) AppToken() (string, error) {
	if c.host.login() == nil {
		return "", errors.New("this node has no login, so it holds no GitHub App")
	}
	return c.host.login().GitHubAppToken()
}

// gitHubService is the node's GitHubService, made the first time it is asked for.
func (g *gitHub) gitHubService() *services.GitHubServer {
	g.serviceOnce.Do(func() {
		g.service = services.NewGitHubServer(gitHubCredentials{host: g.host}, func() bool {
			settings, err := g.host.nodeRecords().GitHub()
			if err != nil {
				g.host.log().Errorw("The node's GitHub settings were not read, so GitHub answers nothing", "error", err)
				return false
			}
			return settings.Enabled
		}, g.host.log().Named("github"))
	})
	return g.service
}

// webhookURL is the URL to paste into the App's webhook settings.
func (g *gitHub) webhookURL(path string) string {
	if g.host.login() == nil {
		return path
	}
	return g.host.login().PublicOrigin() + path
}

func (g *gitHub) signum() sigil.Signum {
	enabled := &protocol.Param{Name: "enabled", Required: true, Says: "true or false."}
	var follows []*protocol.Follows
	if f, err := githubFollowGitHub(); err != nil {
		g.host.log().Errorw("GitHubService's messages were not read against GitHub's description, so parity cannot hold github", "error", err)
	} else {
		follows = append(follows, f)
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "github",
			Description: "The node's GitHub: the GitHubService it spends, the switch that turns GitHub off on the node entirely, and the Actions runner whose builds it runs.",
			Tags:        []string{"github", "app", "runner"},
			Follows:     follows,
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
					Name: "from",
					Does: "Set which GitHub a namespace spends: the system namespace's, or its own.",
					Takes: []*protocol.Param{
						{Name: "namespace", Required: true, Says: "A namespace the node serves, other than system."},
						{Name: "from", Required: true, Says: GitHubFromSystem + " or " + GitHubFromNamespace + "."},
					},
					Answer: "protocol.GitHubFrom",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/from"},
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
				githubFollowSigil(),
				githubAskSigils()[0],
				githubAskSigils()[1],
			},
		},
		Answers: map[string]sigil.Answer{
			"status":       g.githubStatus,
			"node":         g.githubNode,
			"from":         g.githubFrom,
			"runner":       g.githubRunner,
			"webhook":      g.githubWebhook,
			"webhook_path": g.githubWebhookPath,
			"follow":       g.githubFollow,
			"ask":          g.githubAsk,
			"operations":   g.githubOperations,
		},
	}
}

func (g *gitHub) githubStatus(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	status := &protocol.GitHubStatus{
		Enabled:    settings.Enabled,
		Namespaces: []*protocol.GitHubNamespace{},
		Runner:     g.runnerStatus(settings),
	}
	g.host.fillBuilds(status)
	_, status.Webhook = g.gitHubWebhook()
	status.WebhookPath, status.WebhookUrl = settings.WebhookPath, g.webhookURL(settings.WebhookPath)
	follows, err := g.host.nodeRecords().Follows()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	status.Follows = make([]*protocol.GitHubFollow, 0, len(follows))
	for _, f := range follows {
		status.Follows = append(status.Follows, &protocol.GitHubFollow{Repo: f.Repo, Branch: f.Branch, Dispatches: f.Dispatches,
			Workflow: f.Workflow, Ref: f.Ref, Inputs: f.Inputs, Enabled: f.Enabled})
	}
	if g.host.login() == nil {
		return status, nil
	}
	keeper, err := g.host.login().GitHubKeeper()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	held, err := keeper.GitHubTokens()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	from, err := g.host.nodeRecords().GitHubFrom()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, token := range held {
		namespace := strings.Join(token.Namespaces, ",")
		// A namespace set to spend system's GitHub is shown as that, below.
		if from[namespace] == GitHubFromSystem {
			continue
		}
		status.Namespaces = append(status.Namespaces, g.githubRow(ctx, &protocol.GitHubNamespace{
			Namespace: namespace,
			Source:    token.GitHub.Source,
			Login:     token.GitHub.Login,
			MintedBy:  token.MintedBy,
			Revoked:   token.RevokedAt != nil,
		}))
	}
	system, systemHas, err := keeper.GitHubIn(auth.NamespaceSystem)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for namespace, spends := range from {
		if spends != GitHubFromSystem {
			continue
		}
		row := &protocol.GitHubNamespace{Namespace: namespace, Source: GitHubFromSystem}
		if systemHas {
			row.Login, row.MintedBy = system.GitHub.Login, system.MintedBy
		}
		status.Namespaces = append(status.Namespaces, g.githubRow(ctx, row))
	}
	slices.SortFunc(status.Namespaces, func(a, b *protocol.GitHubNamespace) int {
		return strings.Compare(a.GetNamespace(), b.GetNamespace())
	})
	return status, nil
}

// githubRow is a namespace's row with whether its GitHub works, asked of
// GitHub and not remembered, and its rate limit.
func (g *gitHub) githubRow(ctx context.Context, row *protocol.GitHubNamespace) *protocol.GitHubNamespace {
	// TODO(forge): Whether a namespace's token works, and its rate limit, is asked of GitHubService. A forge answers whether it is reached as a namespace, and its limit if it has one (Forge).
	said, err := g.gitHubService().RateLimit(ctx, &protocol.GitHubRateLimitRequest{Namespace: row.Namespace})
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
	return row
}

// githubFrom sets which GitHub a namespace spends: the system namespace's, or its own.
func (g *gitHub) githubFrom(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	from := strings.TrimSpace(sent["from"])
	if from != GitHubFromSystem && from != GitHubFromNamespace {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "from",
			Says: "from is " + GitHubFromSystem + " or " + GitHubFromNamespace + ", and this said " + sent["from"]}
	}
	namespace := strings.TrimSpace(sent["namespace"])
	if namespace == auth.NamespaceSystem {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "namespace", Says: "the system namespace's GitHub is ROOT's own, and comes from nowhere else"}
	}
	served, err := g.host.serves(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "namespace", Says: err.Error()}
	}
	if err := g.host.nodeRecords().SetGitHubFrom(actorOf(ctx), served, from); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubFrom{Namespace: served, From: from}, nil
}

func (g *gitHub) githubNode(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	on, err := strconv.ParseBool(sent["enabled"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "enabled", Says: "enabled is true or false, and this said " + sent["enabled"]}
	}
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.Enabled = on
	if err := g.host.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubNode{Enabled: on}, nil
}

func (g *gitHub) githubRunner(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
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
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.RunnerPath, settings.RunnerEnabled = path, on
	if err := g.host.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	g.StartRunner()
	return &protocol.GitHubRunnerSet{Runner: g.runnerStatus(settings)}, nil
}

func (g *gitHub) githubWebhook(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	if g.host.login() == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no login, so it keeps no webhook secret"}
	}
	secret, err := g.host.login().NewGitHubWebhook(actorOf(ctx))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubWebhook{Secret: secret, Url: g.webhookURL(settings.WebhookPath)}, nil
}

func (g *gitHub) githubWebhookPath(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	path := strings.TrimSpace(sent["path"])
	if err := webhookPath(path); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: err.Error()}
	}
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	settings.WebhookPath = path
	if err := g.host.nodeRecords().SetGitHub(actorOf(ctx), settings); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.GitHubWebhookPath{Url: g.webhookURL(path)}, nil
}

// runnerStatus is the runner as the Actions section shows it.
func (g *gitHub) runnerStatus(settings GitHubSettings) *protocol.GitHubRunner {
	shown := &protocol.GitHubRunner{Path: settings.RunnerPath, Enabled: settings.RunnerEnabled}
	if !settings.RunnerEnabled {
		return shown
	}
	g.runnerMu.Lock()
	runner, failed := g.runner, g.runnerErr
	g.runnerMu.Unlock()
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
	return &protocol.RunnerStats{Path: stats.Path, Name: stats.Name, GithubUrl: stats.GitHubURL,
		Workspaces: stats.Workspaces, Jobs: jobs, Taken: takenBuilds(stats.Taken), Unread: stats.Unread}
}

// takenBuilds is the builds the node took as the GitHub element is given them.
func takenBuilds(builds []grpcplugin.TakenBuild) []*protocol.TakenBuild {
	taken := make([]*protocol.TakenBuild, 0, len(builds))
	for _, build := range builds {
		taken = append(taken, &protocol.TakenBuild{Plugin: build.Plugin, Archive: build.Archive, Digest: build.Digest,
			At: build.At.Format(time.RFC3339Nano), Changed: build.Changed, Older: build.Older, Run: build.Run})
	}
	return taken
}

// StartRunner watches the runner the node's settings name, or stops watching
// when they name none. Called at start and whenever the settings change.
func (g *gitHub) StartRunner() {
	g.stopRunner()
	settings, err := g.host.nodeRecords().GitHub()
	if err != nil {
		g.host.log().Errorw("The node's GitHub settings were not read, so no runner is watched", "error", err)
		return
	}
	if !settings.RunnerEnabled {
		return
	}
	runner, err := grpcplugin.OpenRunner(settings.RunnerPath)
	if err != nil {
		g.runnerFailed(err)
		return
	}
	ctx, cancel := context.WithCancel(g.host.lifetime())
	if err := runner.Watch(ctx, g.host.buildLanded, g.host.log().Named("runner")); err != nil {
		cancel()
		g.runnerFailed(err)
		return
	}
	g.runnerMu.Lock()
	g.runner, g.runnerStop, g.runnerErr = runner, cancel, ""
	g.runnerMu.Unlock()
}

func (g *gitHub) runnerFailed(err error) {
	g.host.log().Errorw("The runner is on and not watched", "error", err)
	g.runnerMu.Lock()
	g.runnerErr = err.Error()
	g.runnerMu.Unlock()
}

func (g *gitHub) stopRunner() {
	g.runnerMu.Lock()
	defer g.runnerMu.Unlock()
	if g.runnerStop != nil {
		g.runnerStop()
	}
	g.runner, g.runnerStop, g.runnerErr = nil, nil, ""
}
