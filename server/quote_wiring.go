package server

import (
	quoted "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/server/quote"
	errors "github.com/teranos/sacred-error"
)

// namespacePrompts is what the person said in a namespace: the human turns of
// its transcripts.
// "make quote read the transcript's human turns?"
func (s *QNTXServer) namespacePrompts(namespace string) ([]string, error) {
	if s.held == nil {
		return nil, errors.New("no namespaces are held to read prompts from")
	}
	store, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "opening %s to read its prompts", namespace)
	}
	return humanTurnsIn(store)
}

// The quote subsystem is optional: quotes are judged in Rust (ats/quote), so a
// build without it (quickdev, or no qntxwasm) leaves quote.provenance and
// quote.removal out, and says so, rather than register built-ins that could
// only fail.
func (s *QNTXServer) quoteCarried() bool {
	if !quoted.Carried {
		s.logger.Infow("quote subsystem not carried: quotes are judged in Rust, which this build leaves out",
			"handlers", []string{"quote.provenance", "quote.removal"})
	}
	return quoted.Carried
}

// setupQuoteProvenance registers the built-in. No schedule: the standing row
// reaches it when a claim arrives.
func (s *QNTXServer) setupQuoteProvenance() {
	if !s.quoteCarried() {
		return
	}
	s.daemon.Registry().Register(quote.NewWatch(s.namespacePrompts, quoted.Judge, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.provenance")))
}

// setupQuoteRemoval registers the built-in. No schedule: the standing row
// reaches it when a commit's removals arrive.
func (s *QNTXServer) setupQuoteRemoval() {
	if !s.quoteCarried() {
		return
	}
	s.daemon.Registry().Register(quote.NewRemoval(s.namespacePrompts, quoted.Judge, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.removal")))
}
