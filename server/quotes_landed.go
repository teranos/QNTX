package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	quotes "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/quote"
	errors "github.com/teranos/sacred-error"
)

// For every pull request that lands on main with its checks green, another
// opens right after it when an agent's words stood inside a person's
// quotation marks: the loop of ADR-055, run off the webhook as a job.

// quoteLandedHandler is the built-in quote.landed, reached by the job the
// webhook enqueues for a merged pull request.
type quoteLandedHandler struct{ server *QNTXServer }

func (h *quoteLandedHandler) Name() string { return quote.LandedHandlerName }

func (h *quoteLandedHandler) Execute(ctx context.Context, job *async.Job) error {
	var landing quote.Landing
	if err := json.Unmarshal(job.Payload, &landing); err != nil {
		return errors.Wrap(err, "quote.landed payload is not a landing")
	}
	s := h.server
	ask := func(operation string, request any) ([]byte, error) { return s.askGitHub(ctx, operation, request) }
	arrivals := quote.NewArrivals(s.namespaceHolding, s.namespaceSaid, quotes.Measure, s.logger.Named("quote.arrival"))
	began := time.Now()
	done, err := quote.NewLanded(ask, arrivals, s.agentWritesFor, s.logger.Named(quote.LandedHandlerName)).Correct(landing)
	if err != nil {
		return errors.Wrapf(err, "quote.landed: %s/%s#%d", landing.Owner, landing.Repo, landing.Number)
	}
	// The cost of the analysis is said with its result, for every analyser
	// over ground's rows to be weighed against the next.
	s.logger.Infow("a landing was read for quotes", "repo", landing.Owner+"/"+landing.Repo, "landing", landing.Name(),
		"agents", done.Agents, "lines", done.Lines, "sure", done.Sure, "likely", done.Likely, "spans", len(done.Spans),
		"opened", done.PullRequest, "skipped", done.Skipped, "took", time.Since(began).String())
	return nil
}

// askGitHub asks GitHubService one operation as the App's installation, and
// hands back GitHub's answer, or the service's own error as one.
func (s *QNTXServer) askGitHub(ctx context.Context, operation string, request any) ([]byte, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, errors.Wrapf(err, "what %s takes did not encode", operation)
	}
	answered, err := s.gitHubService().Ask(services.AsInstallation(ctx), operation, raw)
	if err != nil {
		return nil, err
	}
	var said struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(answered, &said); err != nil {
		return nil, errors.Wrapf(err, "what %s answered did not read", operation)
	}
	if !said.Success {
		return nil, errors.Newf("%s: %s", operation, said.Error)
	}
	return answered, nil
}

// setupQuoteLanded registers the built-in; setupQuote calls it only when the
// build carries the quote subsystem.
func (s *QNTXServer) setupQuoteLanded() {
	s.daemon.Registry().Register(&quoteLandedHandler{server: s})
}

// quoteLanded enqueues the loop for a push to a repository's default branch:
// anything that lands on main, a merged pull request or a commit pushed
// straight to it. Nothing is done on the webhook's own time.
func (s *QNTXServer) quoteLanded(body []byte) error {
	if !quotes.Carried || s.daemon == nil {
		return nil
	}
	var e gitHubQuotedEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return errors.Wrap(err, "the push is not readable JSON")
	}
	if e.Deleted || e.Repository.DefaultBranch == "" || e.Ref != "refs/heads/"+e.Repository.DefaultBranch {
		return nil
	}
	owner, repo, named := strings.Cut(e.Repository.FullName, "/")
	if !named {
		return errors.Newf("the repository %q is not owner/repo", e.Repository.FullName)
	}
	landing := quote.Landing{Owner: owner, Repo: repo, DefaultBranch: e.Repository.DefaultBranch, HeadSha: e.After, BaseSha: e.Before}
	for _, c := range e.Commits {
		landing.Messages = append(landing.Messages, c.Message)
	}
	payload, err := json.Marshal(landing)
	if err != nil {
		return errors.Wrap(err, "the landing did not encode")
	}
	now := time.Now()
	job := &async.Job{ID: quote.LandedHandlerName + ":" + e.Repository.FullName + "@" + e.After,
		HandlerName: quote.LandedHandlerName, Payload: payload, Source: "webhook", Status: async.JobStatusQueued, MadeBy: async.MadeByNoCaller,
		CreatedAt: now, UpdatedAt: now, Pulse: async.NoPulse(), Started: async.NotYet(), Completed: async.NotYet()}
	return errors.Wrapf(s.daemon.GetQueue().Enqueue(job), "quote.landed: enqueueing %s", job.ID)
}
