package server

// "gh side checks need to pass fully and then QNTX should be doing it's own build and install"

import (
	"context"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
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

// commitChecks is what GitHub Actions says of one commit.
type commitChecks struct {
	// states is each check run's newest word, by its name.
	states map[string]string
	// suited is whether Actions made a suite for the commit at all; settled,
	// whether every suite it made completed; failed, a suite or run that did
	// not conclude well.
	suited, settled, refused bool
	failed                   []string
}

// refuse is a suite or run that did not conclude well.
func (c *commitChecks) refuse(what string) {
	c.refused = true
	c.failed = append(c.failed, what)
}

// checksOn asks GitHub what Actions says of repo at sha.
func (s *QNTXServer) checksOn(ctx context.Context, repo, sha string) (commitChecks, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return commitChecks{}, errors.Newf("%q names no repository: owner/repo", repo)
	}
	ctx = services.AsInstallation(ctx)
	suites, err := s.gitHubService().ListCheckSuitesForAGitReference(ctx, &protocol.GitHubListCheckSuitesForAGitReferenceRequest{
		Owner: owner, Repo: name, Ref: sha, AppId: gitHubActionsApp, PerPage: 100,
	})
	if err != nil {
		return commitChecks{}, errors.Wrapf(err, "GitHub was not asked about the check suites on %s@%s", repo, sha)
	}
	if !suites.GetSuccess() {
		return commitChecks{}, errors.Newf("the check suites on %s@%s were not listed: %s", repo, sha, suites.GetError())
	}
	runs, err := s.gitHubService().ListCheckRunsForAGitReference(ctx, &protocol.GitHubListCheckRunsForAGitReferenceRequest{
		Owner: owner, Repo: name, Ref: sha, AppId: gitHubActionsApp, PerPage: 100,
	})
	if err != nil {
		return commitChecks{}, errors.Wrapf(err, "GitHub was not asked about the check runs on %s@%s", repo, sha)
	}
	if !runs.GetSuccess() {
		return commitChecks{}, errors.Newf("the check runs on %s@%s were not listed: %s", repo, sha, runs.GetError())
	}

	said := commitChecks{states: map[string]string{}, settled: true}
	for _, suite := range suites.GetCheckSuites() {
		said.suited = true
		if suite.GetStatus() != "completed" {
			said.settled = false
			continue
		}
		if !concludedWell(suite.GetConclusion()) {
			said.refuse("a suite concluded " + suite.GetConclusion())
		}
	}
	// A check run again is a newer run under the same name; the newest stands.
	newest := map[string]int64{}
	for _, run := range runs.GetCheckRuns() {
		if seen, held := newest[run.GetName()]; held && seen > run.GetId() {
			continue
		}
		newest[run.GetName()] = run.GetId()
		said.states[run.GetName()] = checkState(run.GetStatus(), run.GetConclusion())
	}
	for name, state := range said.states {
		if state == checkFailed {
			said.refuse(name)
		}
	}
	return said, nil
}

// awaitChecks holds b's deploy at its checks until GitHub Actions passed every
// one on the commit pushed, then builds it. A check that fails ends the deploy,
// and so does a newer push.
func (s *QNTXServer) awaitChecks(ctx context.Context, b pluginBuild, repo, sha string, logger *zap.SugaredLogger) {
	began := time.Now()
	for s.deploys.awaiting(b.name, sha) {
		said, err := s.checksOn(ctx, repo, sha)
		if err != nil {
			s.deploys.fail(b.name, err)
			logger.Errorw("A plugin's checks were not read, so it is not built", "plugin", b.name, "repo", repo, "sha", sha, "error", err)
			return
		}
		s.deploys.checksAre(b.name, sha, said.states)
		if said.refused {
			s.deploys.fail(b.name, errors.Newf("checks on %s@%s did not pass: %s", repo, sha, strings.Join(said.failed, ", ")))
			return
		}
		if said.suited && said.settled {
			break
		}
		if !said.suited && time.Since(began) >= checksAppear {
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
