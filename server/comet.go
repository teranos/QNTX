package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/server/comet"
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

// cometsMovedBy rebuilds each comet the node keeps that the push moves, and names them.
func (s *QNTXServer) cometsMovedBy(push forge.Push) []string {
	logger := s.logger.Named("comet")
	dir, err := comet.Dir()
	if err != nil {
		logger.Errorw("A push arrived and the comets were not looked for", "repo", push.Repo.String(), "error", err)
		return []string{}
	}
	kept, err := comet.Kept(dir)
	if err != nil {
		logger.Errorw("A push arrived and the comets the node keeps were not read", "repo", push.Repo.String(), "dir", dir, "error", err)
		return []string{}
	}
	rebuilding := []string{}
	for _, n := range comet.MovedBy(kept, push.Repo.Path, push.Branch(), push.DefaultBranch) {
		if err := s.cometHandler().Start(n); err != nil {
			logger.Errorw("A push moves a comet the node cannot build", "comet", n.Key(), "after", push.After, "error", err)
			continue
		}
		rebuilding = append(rebuilding, n.Key())
	}
	return rebuilding
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
