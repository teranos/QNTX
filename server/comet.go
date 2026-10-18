package server

import (
	"context"
	"net/http"
	"strings"
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

// groundTheNodeAgent builds the ground each agent runs under, where it does not run under one yet.
// Until it does, no turn is said to it (claudePart's ready).
func (s *QNTXServer) groundTheNodeAgent() {
	for _, home := range s.underGround {
		if err := comet.Under(home); err != nil {
			s.logger.Named("comet").Infow("An agent's ground is built", "home", home, "because", err.Error())
			s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL)
		}
	}
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
	// An agent's ground is built from ground's main as every comet is.
	if strings.EqualFold(push.Repo.Path, comet.Ground.Owner+"/"+comet.Ground.Repo) && push.Branch() == comet.Ground.Branch {
		for _, home := range s.underGround {
			s.cometHandler().Builder.GroundAgent(s.ctx, home, s.ownURL)
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
	return n.s.buildRev(ctx, cometSource(source))
}

func (n cometNode) Fetch(ctx context.Context, source comet.Source, rev, tarball string) error {
	return n.s.fetchCore(ctx, cometSource(source), rev, tarball)
}

// TODO(forge): a comet's source is on GitHub until comet.Source names its forge.
// cometSource is a comet's source as a build reads one.
func cometSource(source comet.Source) buildSource {
	return buildSource{Repo: forge.Repo{Host: forge.GitHub, Path: source.Owner + "/" + source.Repo}, Branch: source.Branch}
}

func (cometNode) BuildsDir() (string, error) { return buildsDir() }

func (cometNode) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	return runBuild(ctx, dir, env, name, args...)
}
