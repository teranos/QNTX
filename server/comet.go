package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
	errors "github.com/teranos/sacred-error"
)

// HandleComet is the comet route (ADR-050); package comet answers it.
func (s *QNTXServer) HandleComet(w http.ResponseWriter, r *http.Request) {
	s.cometHandler().ServeHTTP(w, r)
}

// cometHandler is package comet, answering in the node's JSON and building with the node's GitHub App and nix.
func (s *QNTXServer) cometHandler() comet.Handler {
	return comet.Handler{
		Builder: comet.Builder{Node: cometNode{s}, NixBin: nixBin},
		Ctx:     s.ctx,
		Refuse:  writeError,
		Respond: func(w http.ResponseWriter, status int, data any) { respond(w, s.logger, status, data) },
		Log:     s.logger.Named("comet").Infow,
	}
}

// "AS AN A2A AGENT, IT ALSO NEEDS TO SHACKLE ITSELF UNDER GROUND"

// "i wanted ground for the namespace agent under claude first"

// groundHomes is the homes of the agents the node runs under ground: the Node Agent's from its boot,
// a namespace agent's from the first time it is held.
type groundHomes struct {
	mu    sync.Mutex
	homes []string
}

// add keeps home, and says whether it is new.
func (g *groundHomes) add(home string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if slices.Contains(g.homes, home) {
		return false
	}
	g.homes = append(g.homes, home)
	return true
}

// all is every home kept so far.
func (g *groundHomes) all() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.homes)
}

// groundTheAgents builds the ground each agent runs under, where it does not run under one yet.
// Until it does, no turn is said to it (claudePart's ready).
func (s *QNTXServer) groundTheAgents() {
	for _, home := range s.underGround.all() {
		s.groundAgent(home)
	}
}

// groundAgent builds the ground of the agent in home, unless it runs under one, whose settings
// it then writes again.
func (s *QNTXServer) groundAgent(home string) {
	err := comet.Under(home)
	if err != nil {
		s.logger.Named("comet").Infow("An agent's ground is built", "home", home, "because", err.Error())
		s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL, s.keepAgentGround, s.logger.Named("comet").Infow)
		return
	}
	// A ground that stands loads from the store a deploy collects (plugin_roots.go): kept, or built again.
	if err := s.keepAgentGround(home); err != nil {
		s.logger.Named("comet").Infow("An agent's ground is built again: what it loads from was not kept", "home", home, "because", err.Error())
		s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL, s.keepAgentGround, s.logger.Named("comet").Infow)
		return
	}
	// A ground that stands is run by the settings this node writes, not by those an older one wrote.
	if err := comet.AgentSettings(home); err != nil {
		s.logger.Named("comet").Infow("An agent's ground is built again: its settings did not write", "home", home, "because", err.Error())
		s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL, s.keepAgentGround, s.logger.Named("comet").Infow)
	}
}

// keepAgentGround roots every store path the agent's ground binaries load from beside them, and fetches
// any a collection took: on 2026-10-19 a deploy took its glibc, and the ground stood and ran nothing.
func (s *QNTXServer) keepAgentGround(home string) error {
	for _, built := range []string{comet.AgentGround(home), comet.AgentHook(home), comet.AgentSky(home), comet.AgentDrive(home)} {
		data, err := os.ReadFile(built)
		if err != nil {
			return errors.Wrapf(err, "the agent's %s did not read", built)
		}
		fetched, collected, err := rootStorePaths(s.ctx, filepath.Join(nixBin, "nix-store"), built, data, filepath.Join(filepath.Dir(built), storeRootsDir))
		if err != nil {
			return err
		}
		if collected {
			s.logger.Named("comet").Infow("What an agent's ground loads from had been collected, and is fetched and kept again", "home", home, "binary", built, "fetched", fetched)
		}
	}
	return nil
}

// cometsMovedBy rebuilds each comet the node keeps that the push moves, and names them.
func (s *QNTXServer) cometsMovedBy(push forge.Push) []string {
	logger := s.logger.Named("comet")
	dir, err := comet.Dir()
	if err != nil {
		logger.Errorw("A push arrived and the comets were not looked for", "repo", push.Repo.String(), "error", err)
		return []string{}
	}
	kept, err := comet.Names(dir)
	if err != nil {
		logger.Errorw("A push arrived and the comets the node keeps were not read", "repo", push.Repo.String(), "dir", dir, "error", err)
		return []string{}
	}
	rebuilding := []string{}
	// An agent's ground is built from ground's branch as every comet is.
	if strings.EqualFold(push.Repo.Path, comet.Ground.Owner+"/"+comet.Ground.Repo) && push.Branch() == comet.Ground.Branch {
		for _, home := range s.underGround.all() {
			s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL, s.keepAgentGround, s.logger.Named("comet").Infow)
			rebuilding = append(rebuilding, "the ground of the agent in "+home)
		}
	}
	for _, n := range comet.MovedBy(kept, push.Repo.Path, push.Branch(), push.DefaultBranch) {
		if err := s.cometHandler().Start(n); err != nil {
			logger.Errorw("A push moves a comet the node cannot build", "comet", n.Key(), "after", push.After, "error", err)
			continue
		}
		rebuilding = append(rebuilding, n.Key())
	}
	return rebuilding
}

// movedThisWeek is how often owner/repo's default branch moved in the last week: its commits since then, by the node's GitHub App.
func (s *QNTXServer) movedThisWeek(ctx context.Context, owner, repo string) (int, error) {
	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	moved := 0
	for page := int64(1); ; page++ {
		said, err := s.gitHubService().ListCommits(ctx, &protocol.GitHubListCommitsRequest{
			Namespace: auth.NamespaceSystem, Owner: owner, Repo: repo, Since: since, PerPage: 100, Page: page,
		})
		if err != nil {
			return 0, err
		}
		if !said.Success {
			return 0, errors.Newf("GitHub did not list the commits of %s/%s since %s: %s", owner, repo, since, said.Error)
		}
		moved += len(said.Items)
		if len(said.Items) < 100 {
			return moved, nil
		}
	}
}

// cometNode is the node as a comet's build asks it: plugin_build.go's fetching and running.
type cometNode struct{ s *QNTXServer }

func (n cometNode) Latest(ctx context.Context, source comet.Source) (string, error) {
	if n.s.primaryGitHub.held {
		return n.s.primaryGitHub.latest(ctx, source)
	}
	return n.s.buildRev(ctx, cometSource(source))
}

func (n cometNode) Fetch(ctx context.Context, source comet.Source, rev, tarball string) error {
	if n.s.primaryGitHub.held {
		return n.s.primaryGitHub.fetch(ctx, source, rev, tarball)
	}
	return n.s.fetchCore(ctx, cometSource(source), rev, tarball)
}

// TODO(forge): a comet's source is on GitHub until comet.Source names its forge.
// cometSource is a comet's source as a build reads one.
func cometSource(source comet.Source) buildSource {
	return buildSource{Repo: forge.Repo{Host: forge.GitHub, Path: source.Owner + "/" + source.Repo}, Branch: source.Branch}
}

// "cant k use piggyback on what q offers"
// primaryGitHub is a peer's GitHub: its primary's, asked by signing as this node (auth/github_for_peers.go).
type primaryGitHub struct {
	get    auth.GitHubGet
	locate auth.GitHubLocate
	held   bool
}

// latest is the commit source's branch points at now, as the primary asks GitHub.
func (p primaryGitHub) latest(ctx context.Context, source comet.Source) (string, error) {
	asked := githubAPI + "/repos/" + url.PathEscape(source.Owner) + "/" + url.PathEscape(source.Repo) + "/commits?per_page=1&sha=" + url.QueryEscape(source.Branch)
	body, err := p.get(ctx, asked)
	if err != nil {
		return "", errors.Wrapf(err, "the primary did not ask GitHub where %s/%s@%s is", source.Owner, source.Repo, source.Branch)
	}
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", errors.Wrapf(err, "GitHub's commits of %s/%s@%s did not read: %s", source.Owner, source.Repo, source.Branch, body)
	}
	// per_page is 1: the one commit GitHub hands back is where the branch is now.
	for _, commit := range commits {
		return commit.SHA, nil
	}
	return "", errors.Newf("GitHub has no commit for %s/%s@%s", source.Owner, source.Repo, source.Branch)
}

// fetch writes source's archive at rev to tarball: from where GitHub points, as the primary asked it.
func (p primaryGitHub) fetch(ctx context.Context, source comet.Source, rev, tarball string) error {
	asked := githubAPI + "/repos/" + url.PathEscape(source.Owner) + "/" + url.PathEscape(source.Repo) + "/tarball/" + url.PathEscape(rev)
	location, err := p.locate(ctx, asked)
	if err != nil {
		return errors.Wrapf(err, "the primary did not ask GitHub where the archive of %s/%s at %s is", source.Owner, source.Repo, rev)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return errors.Wrapf(err, "where GitHub keeps the archive of %s/%s at %s is no request", source.Owner, source.Repo, rev)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return errors.Wrapf(err, "the archive of %s/%s at %s did not come", source.Owner, source.Repo, rev)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		said, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return errors.Wrapf(readErr, "the archive of %s/%s at %s answered %d, and what it said did not read", source.Owner, source.Repo, rev, resp.StatusCode)
		}
		return errors.Newf("the archive of %s/%s at %s answered %d: %s", source.Owner, source.Repo, rev, resp.StatusCode, said)
	}
	file, err := os.Create(tarball)
	if err != nil {
		return errors.Wrapf(err, "failed to make %s", tarball)
	}
	if written, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return errors.Wrapf(err, "failed to write %s/%s at %s to %s after %d bytes", source.Owner, source.Repo, rev, tarball, written)
	}
	if err := file.Close(); err != nil {
		return errors.Wrapf(err, "failed to finish writing %s", tarball)
	}
	return nil
}

func (cometNode) BuildsDir() (string, error) { return buildsDir() }

func (cometNode) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	return runBuild(ctx, dir, env, name, args...)
}
