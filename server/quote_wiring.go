package server

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/teranos/QNTX/ats"
	quotes "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/quote"
	errors "github.com/teranos/sacred-error"
)

// namespacePrompts is what the person said in a namespace: the human turns of
// its transcripts.
// "make quote read the transcript's human turns?"
func (s *QNTXServer) namespaceSaid(namespace string) ([]quote.Said, error) {
	if s.held == nil {
		return nil, errors.New("no namespaces are held to read prompts from")
	}
	store, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "opening %s to read its prompts", namespace)
	}
	return humanSaidIn(store)
}

// namespaceHolding is the held namespace with a row of ground's for the
// project, by the context ground writes, project:owner/repo. None means ground
// does not know the project.
func (s *QNTXServer) namespaceHolding(project string) (string, bool) {
	if s.held == nil || s.held.Known() == nil {
		return "", false
	}
	held, err := s.held.Known().List()
	if err != nil {
		s.logger.Warnw("the namespaces could not be listed to find a project's rows", "project", project, "error", err)
		return "", false
	}
	for _, ns := range held {
		store, err := s.held.Read(ns.Name)
		if err != nil {
			continue
		}
		found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
			Contexts: []string{"project:" + project}, Limit: 1})
		if err == nil && len(found) > 0 {
			return ns.Name, true
		}
	}
	return "", false
}

// gitHubQuotedEvent is the part of a push or pull_request delivery that
// carries words a person may be quoted in.
type gitHubQuotedEvent struct {
	After      string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
	PullRequest *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	} `json:"pull_request"`
}

// quotesFromGitHub measures the quoted spans a delivery carried: a push's
// commit messages, a pull request's title and body. Nil when this build does
// not carry the quote subsystem, or the event carries no words.
func (s *QNTXServer) quotesFromGitHub(_ context.Context, event string, body []byte) (*quote.Arrived, error) {
	if !quotes.Carried {
		return nil, nil
	}
	var e gitHubQuotedEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, errors.Wrapf(err, "the %s event is not readable JSON", event)
	}
	arrival := quote.Arrival{Project: e.Repository.FullName}
	switch event {
	case "push":
		arrival.Ref = e.After
		for _, c := range e.Commits {
			arrival.Texts = append(arrival.Texts, c.Message)
		}
	case "pull_request":
		if e.PullRequest == nil {
			return nil, nil
		}
		arrival.Ref = "pull/" + strconv.Itoa(e.PullRequest.Number)
		arrival.Texts = []string{e.PullRequest.Title, e.PullRequest.Body}
	default:
		return nil, nil
	}
	arrived, err := quote.NewArrivals(s.namespaceHolding, s.namespaceSaid, quotes.Measure, s.logger.Named("quote.arrival")).Measure(arrival)
	if err != nil {
		return nil, err
	}
	s.logger.Infow("quoted spans measured on arrival", "project", arrived.Project, "ref", arrived.Ref,
		"known", arrived.Known, "namespace", arrived.Namespace, "spans", len(arrived.Spans))
	return &arrived, nil
}

// The quote subsystem is optional: quotes are measured in Rust (ats/quote), so a
// build without it (quickdev, or no qntxquote) leaves quote.provenance and
// quote.removal out, and says so once, rather than register built-ins that
// could only fail.
func (s *QNTXServer) setupQuote() {
	if !quotes.Carried {
		s.logger.Infow("quote subsystem not carried: quotes are measured in Rust, which this build leaves out",
			"handlers", []string{"quote.provenance", "quote.removal"})
		return
	}
	s.setupQuoteProvenance()
	s.setupQuoteRemoval()
	s.setupQuoteLanded()
}

// setupQuoteProvenance registers the built-in. No schedule: the standing row
// reaches it when a claim arrives.
func (s *QNTXServer) setupQuoteProvenance() {
	s.daemon.Registry().Register(quote.NewWatch(s.namespaceSaid, quotes.Measure, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.provenance")))
}

// setupQuoteRemoval registers the built-in. No schedule: the standing row
// reaches it when a commit's removals arrive.
func (s *QNTXServer) setupQuoteRemoval() {
	s.daemon.Registry().Register(quote.NewRemoval(s.namespaceSaid, quotes.Measure, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.removal")))
}
