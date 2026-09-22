package server

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// ci.watch is the built-in the standing CI watcher reaches. ground's hook
// attests a push to a branch with CI; sky streams that row here; this waits on
// the run and leaves what it concluded on the row, for the token that attested
// it. The laptop cannot be reached from this node, and the row is what it
// already polls.

// ciWatchCeiling is how long a run is waited on before it is given up as news
// that never came. A run that long is its own failure.
const ciWatchCeiling = 2 * time.Hour

// The two gh asks, as ground made them. The first is the median and ninetieth
// percentile of the branch's last twenty runs, in seconds, "p50 p90"; the
// second is the run for one commit.
const ciPercentilesJQ = `[.[] | ((.updatedAt | fromdateiso8601) - (.startedAt | fromdateiso8601))] | sort | length as $n | if $n == 0 then "0 0" else "\(.[($n*5/10|floor)]) \(.[($n*9/10|floor)])" end`

// pickAdaptiveSleep is ground's, ported as-is: quiet before the median,
// looking in the likely window, urgent past the ninetieth.
func pickAdaptiveSleep(elapsed, p50, p90 int64) int64 {
	if p50 <= 0 {
		return 2
	}
	if elapsed < p50 {
		return 30
	}
	if p90 <= 0 || elapsed < p90 {
		return 5
	}
	return 2
}

// ghRun runs gh with the arguments given, argv[0] included. gh is installed on
// the node and carries its own auth, so nothing here holds a GitHub token.
func ghRun(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("nothing to run")
	}
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, errors.Wrapf(err, "%s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, errors.Wrapf(err, "%s", strings.Join(args, " "))
	}
	return out, nil
}

// sleepUnder waits, or stops when the context does.
func sleepUnder(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type ciWatchHandler struct {
	run    func(ctx context.Context, args ...string) ([]byte, error)
	sleep  func(ctx context.Context, d time.Duration) error
	news   *newsLog
	logger *zap.SugaredLogger
}

func (h *ciWatchHandler) Name() string { return watcher.CIWatchHandlerName }

// ciRun is the one field set gh is asked for about a run.
type ciRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Name       string `json:"name"`
	URL        string `json:"url"`
}

func (h *ciWatchHandler) Execute(ctx context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return errors.Wrap(err, "ci.watch payload is not an attestation")
	}

	repo := attrString(as.Attributes, "repo")
	branch := attrString(as.Attributes, "branch")
	sha := attrString(as.Attributes, "sha")
	if repo == "" || branch == "" || sha == "" {
		return errors.Newf("ci.watch: attestation %s names no push: repo=%q branch=%q sha=%q", as.ID, repo, branch, sha)
	}
	if len(as.Actors) == 0 || as.Actors[0] == "" {
		return errors.Newf("ci.watch: attestation %s has no actor to address the result to", as.ID)
	}
	caller := as.Actors[0]

	pushedAt := as.Timestamp
	if pushedAt.IsZero() {
		pushedAt = time.Now()
	}

	p50, p90 := h.percentiles(ctx, repo, branch)

	deadline := time.Now().Add(ciWatchCeiling)
	for {
		run, found, err := h.runFor(ctx, repo, branch, sha)
		if err != nil {
			return err
		}
		if found && run.Status == "completed" {
			h.leave(as, caller, repo, branch, sha, run, pushedAt)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Newf("ci.watch: %s@%s on %s did not conclude within %s", branch, sha, repo, ciWatchCeiling)
		}
		elapsed := int64(time.Since(pushedAt).Seconds())
		wait := time.Duration(pickAdaptiveSleep(elapsed, p50, p90)) * time.Second
		if err := h.sleep(ctx, wait); err != nil {
			return errors.Wrapf(err, "ci.watch: stopped waiting on %s@%s", branch, sha)
		}
	}
}

// percentiles is how long this branch's runs have been taking. Unknown is
// 0 0, which pickAdaptiveSleep reads as no history.
func (h *ciWatchHandler) percentiles(ctx context.Context, repo, branch string) (int64, int64) {
	out, err := h.run(ctx, "gh", "-R", repo, "run", "list", "--branch", branch,
		"--limit", "20", "--json", "startedAt,updatedAt", "--jq", ciPercentilesJQ)
	if err != nil {
		h.logger.Warnw("ci.watch could not read the branch's run history; waiting without it",
			"repo", repo, "branch", branch, "error", err)
		return 0, 0
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0
	}
	p50, err1 := strconv.ParseInt(fields[0], 10, 64)
	p90, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return p50, p90
}

// runFor is the run github has for this commit on this branch, if it has one yet.
func (h *ciWatchHandler) runFor(ctx context.Context, repo, branch, sha string) (ciRun, bool, error) {
	out, err := h.run(ctx, "gh", "-R", repo, "run", "list", "--branch", branch, "--commit", sha,
		"--limit", "1", "--json", "status,conclusion,name,url")
	if err != nil {
		return ciRun{}, false, errors.Wrapf(err, "ci.watch: asking github for %s@%s", branch, sha)
	}
	var runs []ciRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return ciRun{}, false, errors.Wrapf(err, "ci.watch: gh answered for %s@%s with something other than runs: %q", branch, sha, string(out))
	}
	if len(runs) == 0 {
		return ciRun{}, false, nil
	}
	return runs[0], true, nil
}

// leave puts the conclusion on the row for the caller. There is no branch on
// green versus red: the result is the event.
func (h *ciWatchHandler) leave(as types.As, caller, repo, branch, sha string, run ciRun, pushedAt time.Time) {
	symbol := SymbolUnwell
	if run.Conclusion == "success" {
		symbol = SymbolWell
	}
	shortSha := sha
	if len(shortSha) > 7 {
		shortSha = shortSha[:7]
	}
	now := time.Now()
	h.news.leave(News{
		ID:  as.ID,
		For: caller,
		Item: StatusItem{
			Name:   "ci",
			Note:   run.Conclusion + " " + branch + " " + shortSha,
			Symbol: symbol,
		},
		Detail: map[string]any{
			"repo":       repo,
			"branch":     branch,
			"sha":        sha,
			"conclusion": run.Conclusion,
			"run":        run.Name,
			"url":        run.URL,
			"pushed_at":  pushedAt.UTC().Format(time.RFC3339),
			"took_s":     int64(now.Sub(pushedAt).Seconds()),
			"session":    sessionOf(as.Contexts),
		},
		UntilMs: now.Add(newsHold).UnixMilli(),
	})
	h.logger.Infow("ci.watch left news on the row",
		"repo", repo, "branch", branch, "sha", shortSha, "conclusion", run.Conclusion, "for", caller)
}

// sessionOf is the session context ground wrote, so the laptop can hand the
// item to the session that pushed rather than to every session in the project.
func sessionOf(contexts []string) string {
	for _, c := range contexts {
		if strings.HasPrefix(c, "session:") {
			return strings.TrimPrefix(c, "session:")
		}
	}
	return ""
}

// setupCIWatch registers the built-in. No schedule: the standing watcher
// reaches it on arrival.
func (s *QNTXServer) setupCIWatch() {
	if s.daemon == nil {
		return
	}
	if s.news == nil {
		s.news = newNewsLog()
	}
	s.daemon.Registry().Register(&ciWatchHandler{
		run:    ghRun,
		sleep:  sleepUnder,
		news:   s.news,
		logger: s.logger.Named("ci.watch"),
	})
	s.logger.Infow("Registered ci.watch built-in")
}
