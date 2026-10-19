package server

// "gh side checks need to pass fully and then QNTX should be doing it's own build and install"

import (
	"context"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

const (
	// deployChecking is a pushed deploy waiting on GitHub's checks.
	deployChecking = "checks"
	// gitHubActionsApp is GitHub Actions' App: the checks a push waits on are the ones it runs.
	gitHubActionsApp = 15368
	checksEvery      = 10 * time.Second
	// Actions made its suite 1 to 5 seconds after each push measured. A commit
	// with none a minute on has nothing running on it.
	checksAppear = time.Minute
)

// checkWords are each check's newest word, as the deploy records them.
func checkWords(states map[string]forge.CheckState) map[string]string {
	words := make(map[string]string, len(states))
	for name, state := range states {
		words[name] = string(state)
	}
	return words
}

// commitChecks is what the forge's own CI says of the commit pushed.
func (s *QNTXServer) commitChecks(ctx context.Context, repo forge.Repo, sha string) (forge.Checks, error) {
	host, err := s.forges().For(repo)
	if err != nil {
		return forge.Checks{}, err
	}
	return host.Checks(ctx, forge.AsApp, repo, sha, forge.OwnCI)
}

// awaitChecks holds b's deploy at its checks until GitHub Actions passed every
// one on the commit pushed, then builds it. A check that fails ends the deploy,
// and so does a newer push.
func (s *QNTXServer) awaitChecks(ctx context.Context, b pluginBuild, repo forge.Repo, sha string, logger *zap.SugaredLogger) {
	began := time.Now()
	for s.deploys.awaiting(b.name, sha) {
		said, err := s.commitChecks(ctx, repo, sha)
		if err != nil {
			s.deploys.fail(b.name, err)
			logger.Errorw("A plugin's checks were not read, so it is not built", "plugin", b.name, "repo", repo.String(), "sha", sha, "error", err)
			return
		}
		s.deploys.checksAre(b.name, sha, checkWords(said.States))
		if len(said.Failed) > 0 {
			s.deploys.fail(b.name, errors.Newf("checks on %s@%s did not pass: %s", repo, sha, strings.Join(said.Failed, ", ")))
			return
		}
		if said.Ran && said.Settled {
			break
		}
		if !said.Ran && time.Since(began) >= checksAppear {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checksEvery):
		}
	}
	if !s.deploys.checksPassed(b.name, sha) {
		return
	}
	// Queued while another plugin's build holds the node's one build.
	s.building.Lock()
	defer s.building.Unlock()
	s.buildIfMoved(ctx, b, logger)
}
