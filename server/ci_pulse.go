package server

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
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
	run   func(ctx context.Context, args ...string) ([]byte, error)
	sleep func(ctx context.Context, d time.Duration) error
	news  *newsLog
	// Who the token that attested the push speaks for. The row is asked as a
	// person — their session or their own token — and the ground token that
	// streamed the push cannot read the row at all; the news is filed under
	// the person so it is found. Nil files it under the DID.
	mintedBy func(did string) (string, bool)
	logger   *zap.SugaredLogger
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
		runs, err := h.runsFor(ctx, repo, branch, sha)
		if err != nil {
			return err
		}
		if len(runs) > 0 && allConcluded(runs) {
			h.leave(as, caller, repo, branch, sha, runs, pushedAt)
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

// A push starts one run per workflow file. Thirty covers a dozen workflows.
const ciRunsPage = "30"

// runsFor is every run github has for this commit, none yet included.
func (h *ciWatchHandler) runsFor(ctx context.Context, repo, branch, sha string) ([]ciRun, error) {
	out, err := h.run(ctx, "gh", "-R", repo, "run", "list", "--branch", branch, "--commit", sha,
		"--limit", ciRunsPage, "--json", "status,conclusion,name,url")
	if err != nil {
		return nil, errors.Wrapf(err, "ci.watch: asking github for %s@%s", branch, sha)
	}
	var runs []ciRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return nil, errors.Wrapf(err, "ci.watch: gh answered for %s@%s with something other than runs: %q", branch, sha, string(out))
	}
	return runs, nil
}

// The push is concluded when every workflow it started has. Reading one run
// read whichever finished first, so a red lint beside a green deploy was
// never seen and the row said green.
func allConcluded(runs []ciRun) bool {
	for _, r := range runs {
		if r.Status != "completed" {
			return false
		}
	}
	return true
}

// verdict is the worst conclusion among the runs and every run that did not
// succeed, by name. Naming only the first would keep the rest hidden.
func verdict(runs []ciRun) (conclusion string, failed []ciRun) {
	conclusion = "success"
	for _, r := range runs {
		if r.Conclusion == "success" || r.Conclusion == "skipped" {
			continue
		}
		failed = append(failed, r)
		if conclusion == "success" || r.Conclusion == "failure" {
			conclusion = r.Conclusion
		}
	}
	return conclusion, failed
}

// leave puts the conclusion on the row for the caller. There is no branch on
// green versus red: the result is the event.
func (h *ciWatchHandler) leave(as types.As, caller, repo, branch, sha string, runs []ciRun, pushedAt time.Time) {
	conclusion, failed := verdict(runs)
	symbol := SymbolUnwell
	if conclusion == "success" {
		symbol = SymbolWell
	}
	shortSha := sha
	if len(shortSha) > 7 {
		shortSha = shortSha[:7]
	}
	note := conclusion + " " + branch + " " + shortSha
	if len(failed) > 0 {
		note += " " + strconv.Itoa(len(failed)) + "/" + strconv.Itoa(len(runs))
	}
	workflows := make([]map[string]string, 0, len(runs))
	for _, r := range runs {
		workflows = append(workflows, map[string]string{"name": r.Name, "conclusion": r.Conclusion, "url": r.URL})
	}
	url := ""
	if len(failed) > 0 {
		url = failed[0].URL
	} else if len(runs) > 0 {
		url = runs[0].URL
	}
	now := time.Now()
	addressee := caller
	if h.mintedBy != nil {
		if who, ok := h.mintedBy(caller); ok {
			addressee = who
		}
	}
	// The row's id is per session — ground writes one ci-status row per
	// session and replaces it on every push — so the id here carries the
	// commit as well, or the laptop would take a second push's result for the
	// first's and write nothing.
	h.news.leave(News{
		ID:  as.ID + ":" + shortSha,
		For: addressee,
		Item: StatusItem{
			Name:   "ci",
			Note:   note,
			Symbol: symbol,
		},
		Detail: map[string]any{
			"repo":       repo,
			"branch":     branch,
			"sha":        sha,
			"conclusion": conclusion,
			"workflows":  workflows,
			"url":        url,
			"pushed_at":  pushedAt.UTC().Format(time.RFC3339),
			"took_s":     int64(now.Sub(pushedAt).Seconds()),
			"session":    sessionOf(as.Contexts),
		},
		UntilMs: now.Add(newsHold).UnixMilli(),
	})
	h.logger.Infow("ci.watch left news on the row",
		"repo", repo, "branch", branch, "sha", shortSha, "conclusion", conclusion,
		"workflows", len(runs), "failed", len(failed), "for", addressee, "attested_by", caller)
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

// ciWatchRearmWindow is how far back the store is read at boot. A restart
// loses the goroutine that was waiting on a run and the news log with it, and
// every push to QNTX itself restarts this node before its own CI concludes —
// so without the read-back no push to QNTX would ever be reported. Three
// hours is past the ceiling, so nothing a live process would still be
// waiting on is left behind.
const ciWatchRearmWindow = 3 * time.Hour

// ciStatusSince is every push attested since the moment given, oldest first.
// A store that will not answer is an error, not an empty namespace: the two
// read the same, and one of them is a push nobody will ever be told about.
func ciStatusSince(store namespaces.Reading, since time.Time) ([]*types.As, error) {
	if store == nil {
		return nil, errors.New("no store to read pushes from")
	}
	found, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{watcher.CIPushedPredicate},
		TimeStart:  &since,
		Limit:      100,
	})
	if err != nil {
		return nil, errors.Wrap(err, "reading the pushes to re-arm on")
	}
	// The window is checked here as well: not every backend honours
	// TimeStart, and a push is dated by push_time, ground's own stamp, before
	// the row's timestamp.
	recent := make([]*types.As, 0, len(found))
	for _, as := range found {
		at := as.Timestamp
		if v, ok := as.Attributes["push_time"].(float64); ok && v > 0 {
			at = time.Unix(int64(v), 0)
		}
		if at.Before(since) {
			continue
		}
		recent = append(recent, as)
	}
	return recent, nil
}

// rearmFailed puts a re-arm that could not read on the row, where a failing
// handler goes. A log line is not enough: the log is ROOT's, and the row is
// what the person whose push went unreported is looking at.
func (s *QNTXServer) rearmFailed(namespace string, err error) {
	s.logger.Errorw("ci.watch could not re-arm", "namespace", namespace, "error", err)
	s.noteHandlerFailure(HandlerFailure{
		Handler:     watcher.CIWatchHandlerName,
		ExecutionID: "rearm:" + namespace,
		Error:       "re-arm in " + namespace + ": " + err.Error(),
		Details:     errors.GetAllDetails(err),
	})
}

// setupCIWatch registers the built-in. No schedule: the standing watcher
// reaches it on arrival. The pushes of the last hours are waited on again,
// because whatever was waiting on them before this process is gone.
func (s *QNTXServer) setupCIWatch() {
	if s.daemon == nil {
		return
	}
	if s.news == nil {
		s.news = newNewsLog()
	}
	h := &ciWatchHandler{
		run:      ghRun,
		sleep:    sleepUnder,
		news:     s.news,
		mintedBy: s.authHandler.MintedBy,
		logger:   s.logger.Named("ci.watch"),
	}
	s.daemon.Registry().Register(h)
	s.logger.Infow("Registered ci.watch built-in")

	// Every namespace the node knows, not the one it serves by default: the
	// ground token attests in its own, and a push is wherever its token acts.
	since := time.Now().Add(-ciWatchRearmWindow)
	var recent []*types.As
	if s.held != nil {
		found, err := ciStatusSince(s.held.Served(), since)
		if err != nil {
			s.rearmFailed(auth.NamespaceDefault, err)
		}
		recent = append(recent, found...)
		if known := s.held.Known(); known != nil {
			listed, err := known.List()
			if err != nil {
				s.rearmFailed("*", errors.Wrap(err, "listing the namespaces"))
			}
			for _, ns := range listed {
				if ns.Name == auth.NamespaceDefault || ns.Name == auth.NamespaceSystem {
					continue
				}
				store, err := s.held.Read(ns.Name)
				if err != nil {
					s.rearmFailed(ns.Name, err)
					continue
				}
				found, err := ciStatusSince(store, since)
				if err != nil {
					s.rearmFailed(ns.Name, err)
					continue
				}
				recent = append(recent, found...)
			}
		} else {
			s.logger.Infow("ci.watch re-arms on the served namespace only; this backend keeps one")
		}
	}
	for _, as := range recent {
		as := as
		payload, err := json.Marshal(as)
		if err != nil {
			s.logger.Warnw("ci.watch could not re-arm a push", "id", as.ID, "error", err)
			continue
		}
		sacred.Go("ci.watch rearm "+as.ID, func() {
			job := &async.Job{ID: "rearm:" + as.ID, HandlerName: watcher.CIWatchHandlerName, Payload: payload, Source: "boot"}
			if err := h.Execute(s.ctx, job); err != nil {
				s.noteHandlerFailure(HandlerFailure{Handler: watcher.CIWatchHandlerName, ExecutionID: job.ID,
					Error: err.Error(), Details: errors.GetAllDetails(err)})
			}
		})
	}
	if len(recent) > 0 {
		s.logger.Infow("ci.watch re-armed on the pushes of the last hours", "pushes", len(recent))
	}
}
