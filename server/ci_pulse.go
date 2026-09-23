package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// ci.watch is the built-in the standing CI watcher reaches. ground's hook
// attests a push to a branch with CI; sky streams that row here; this waits on
// the run and leaves what it concluded on the row, for the token that attested
// it. The laptop cannot be reached from this node, and the row is what it
// already polls.

// ciWatchCeiling is how long a run is waited on before it is given up as news
// that never came. A run that long is its own failure.
const ciWatchCeiling = 2 * time.Hour

// GitHub is asked directly. "eliminate gh dependency": the box runs no gh,
// its service has no PATH to find one on, and the node already holds a token
// for github.com — the one that fetches private plugin repos.
const githubAPI = "https://api.github.com"

// githubHost is the forge the plugin access token is configured for, and the
// one CI runs are asked of.
const githubHost = "github.com"

// githubToken is the credential configured for github.com under
// [[plugin.access_token]], resolved the way a plugin fetch resolves it. None
// configured is no token: a public repository answers without one.
func githubToken(ctx context.Context) (string, error) {
	ref, err := config.PluginAccessToken(githubHost)
	if err != nil {
		return "", errors.Wrapf(err, "reading the access token configured for %s", githubHost)
	}
	if ref == "" {
		return "", nil
	}
	token, err := secretref.Resolve(ctx, ref)
	if err != nil {
		return "", errors.Wrapf(err, "resolving the access token configured for %s", githubHost)
	}
	return token, nil
}

// rateLimited is GitHub refusing until a moment: the hour's quota is spent.
// The quota is the user's, shared with every tool of theirs, so the ask is
// not failed — it waits.
type rateLimited struct {
	reset time.Time
}

func (r rateLimited) Error() string {
	return "github's rate limit is spent until " + r.reset.UTC().Format(time.RFC3339)
}

// githubPace is how often this process asks GitHub, all pushes together. The
// quota is 5000 an hour for the user and the laptop spends it too; the first
// hour ci.watch ran on the node it spent all of it, ten pushes each asking
// every two seconds. Twenty a minute across every push leaves most of the
// hour to the person.
var githubPace = rate.NewLimiter(rate.Every(3*time.Second), 1)

// githubGet is one GET of the API, the body whole, at the shared pace. A
// status other than 200 is GitHub's own answer and is returned in its words;
// a spent quota is returned as the moment it comes back.
func githubGet(ctx context.Context, token, url string) ([]byte, error) {
	if err := githubPace.Wait(ctx); err != nil {
		return nil, errors.Wrap(err, "waiting for a turn to ask github")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "building the request for %s", url)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "asking %s", url)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, errors.Wrapf(err, "reading github's answer for %s", url)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if reset, ok := rateLimitReset(resp.Header); ok {
			return nil, rateLimited{reset: reset}
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("github answered %d for %s: %s", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// rateLimitReset is when a spent quota comes back, from GitHub's headers: the
// primary limit's reset when remaining is zero, or a Retry-After for the
// secondary one. False is a refusal that is not about the quota.
func rateLimitReset(h http.Header) (time.Time, bool) {
	if h.Get("X-RateLimit-Remaining") == "0" {
		if unix, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return time.Unix(unix, 0), true
		}
	}
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs > 0 {
		return time.Now().Add(time.Duration(secs) * time.Second), true
	}
	return time.Time{}, false
}

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
	// get is one GET of GitHub's API with the token given; token is the
	// credential for github.com, resolved once per push.
	get   func(ctx context.Context, token, url string) ([]byte, error)
	token func(ctx context.Context) (string, error)
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

// ciRun is what is read of a workflow run, in GitHub's own field names.
type ciRun struct {
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	Name         string    `json:"name"`
	URL          string    `json:"html_url"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ciRuns is the shape GitHub lists runs in.
type ciRuns struct {
	WorkflowRuns []ciRun `json:"workflow_runs"`
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
	addressee := h.addressee(caller)
	shortSha := sha
	if len(shortSha) > 7 {
		shortSha = shortSha[:7]
	}
	watchingID := as.ID + ":" + shortSha + ":watching"

	pushedAt := as.Timestamp
	if pushedAt.IsZero() {
		pushedAt = time.Now()
	}

	// On the row from the first moment, so a wait and a push nobody watched
	// do not look the same from the laptop. Quiet: nothing is written down
	// for it and no session is woken by it.
	h.watching(watchingID, addressee, "watching "+branch+" "+shortSha)
	defer h.news.drop(watchingID)

	token, err := h.token(ctx)
	if err != nil {
		return errors.Wrap(err, "ci.watch")
	}

	p50, p90 := h.percentiles(ctx, token, repo, branch)

	// ground's row carries the sha as git's push line printed it, short. The
	// runs endpoint filters on the whole sha and a short one matches nothing:
	// "watching main 162d82f 0 runs", on every push, until the ceiling.
	fullSha, err := h.fullSha(ctx, token, repo, sha)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(ciWatchCeiling)
	for {
		runs, err := h.runsFor(ctx, token, repo, fullSha)
		var spent rateLimited
		if errors.As(err, &spent) {
			// The quota is the person's, and it comes back. A push is not
			// unreported because the hour's asks were used up.
			wait := time.Until(spent.reset) + time.Second
			if wait < time.Second {
				wait = time.Second
			}
			h.logger.Warnw("ci.watch waits for github's rate limit to reset",
				"repo", repo, "sha", sha, "reset", spent.reset.UTC().Format(time.RFC3339))
			h.watching(watchingID, addressee, "quota until "+spent.reset.UTC().Format("15:04")+" "+branch+" "+shortSha)
			if err := h.sleep(ctx, wait); err != nil {
				return errors.Wrapf(err, "ci.watch: stopped waiting on %s@%s", branch, sha)
			}
			continue
		}
		if err != nil {
			return err
		}
		if len(runs) > 0 && allConcluded(runs) {
			h.leave(as, addressee, caller, repo, branch, sha, runs, pushedAt)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Newf("ci.watch: %s@%s on %s did not conclude within %s", branch, sha, repo, ciWatchCeiling)
		}
		elapsed := int64(time.Since(pushedAt).Seconds())
		wait := time.Duration(pickAdaptiveSleep(elapsed, p50, p90)) * time.Second
		h.watching(watchingID, addressee, "watching "+branch+" "+shortSha+" "+strconv.Itoa(len(runs))+" runs")
		if err := h.sleep(ctx, wait); err != nil {
			return errors.Wrapf(err, "ci.watch: stopped waiting on %s@%s", branch, sha)
		}
	}
}

// addressee is who the news is for: the person the attesting token speaks
// for, or the DID itself when nothing says.
func (h *ciWatchHandler) addressee(caller string) string {
	if h.mintedBy != nil {
		if who, ok := h.mintedBy(caller); ok {
			return who
		}
	}
	return caller
}

// watching puts what this push is at on the row, quietly, held for as long
// as one poll interval can be plus the hold — refreshed on every turn of the
// loop, so a wait that ended without a verdict expires off the row on its own.
func (h *ciWatchHandler) watching(id, addressee, note string) {
	h.news.leave(News{
		ID:      id,
		For:     addressee,
		Item:    StatusItem{Name: "ci", Note: note, Symbol: SymbolWell},
		UntilMs: time.Now().Add(ciWatchCeiling + newsHold).UnixMilli(),
		Quiet:   true,
	})
}

// percentiles is how long this branch's runs have been taking: the median and
// ninetieth percentile of the last twenty completed, in seconds, as ground
// computed them. Unknown is 0 0, which pickAdaptiveSleep reads as no history.
func (h *ciWatchHandler) percentiles(ctx context.Context, token, repo, branch string) (int64, int64) {
	url := githubAPI + "/repos/" + repo + "/actions/runs?branch=" + branch + "&status=completed&per_page=20"
	out, err := h.get(ctx, token, url)
	if err != nil {
		h.logger.Warnw("ci.watch could not read the branch's run history; waiting without it",
			"repo", repo, "branch", branch, "error", err)
		return 0, 0
	}
	var listed ciRuns
	if err := json.Unmarshal(out, &listed); err != nil {
		return 0, 0
	}
	return percentilesOf(listed.WorkflowRuns)
}

// percentilesOf is ground's arithmetic over the durations: sorted, the
// element at half and the element at nine tenths.
func percentilesOf(runs []ciRun) (int64, int64) {
	durations := make([]int64, 0, len(runs))
	for _, r := range runs {
		if r.RunStartedAt.IsZero() || r.UpdatedAt.Before(r.RunStartedAt) {
			continue
		}
		durations = append(durations, int64(r.UpdatedAt.Sub(r.RunStartedAt).Seconds()))
	}
	if len(durations) == 0 {
		return 0, 0
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	n := len(durations)
	return durations[n*5/10], durations[n*9/10]
}

// fullSha is the whole sha of a commit named by a prefix, from the commits
// endpoint, which takes either. A whole sha is handed back as it is.
func (h *ciWatchHandler) fullSha(ctx context.Context, token, repo, sha string) (string, error) {
	if len(sha) >= 40 {
		return sha, nil
	}
	out, err := h.get(ctx, token, githubAPI+"/repos/"+repo+"/commits/"+sha)
	if err != nil {
		return "", errors.Wrapf(err, "ci.watch: resolving %s on %s to a whole sha", sha, repo)
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(out, &commit); err != nil || commit.SHA == "" {
		return "", errors.Newf("ci.watch: github answered for %s on %s with no sha: %q", sha, repo, firstN(string(out), 200))
	}
	return commit.SHA, nil
}

// A push starts one run per workflow file. Thirty covers a dozen workflows.
const ciRunsPage = "30"

// runsFor is every run github has for this commit, none yet included.
func (h *ciWatchHandler) runsFor(ctx context.Context, token, repo, sha string) ([]ciRun, error) {
	url := githubAPI + "/repos/" + repo + "/actions/runs?head_sha=" + sha + "&per_page=" + ciRunsPage
	out, err := h.get(ctx, token, url)
	if err != nil {
		return nil, errors.Wrapf(err, "ci.watch: asking github for %s@%s", repo, sha)
	}
	var listed ciRuns
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, errors.Wrapf(err, "ci.watch: github answered for %s@%s with something other than runs: %q", repo, sha, firstN(string(out), 200))
	}
	return listed.WorkflowRuns, nil
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
func (h *ciWatchHandler) leave(as types.As, addressee, caller, repo, branch, sha string, runs []ciRun, pushedAt time.Time) {
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
		get:      githubGet,
		token:    githubToken,
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
