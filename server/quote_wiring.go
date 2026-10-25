package server

import (
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

// setupQuoteProvenance registers the built-in. No schedule: the standing row
// reaches it when a claim arrives.
func (s *QNTXServer) setupQuoteProvenance() {
	s.daemon.Registry().Register(quote.NewWatch(s.namespacePrompts, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.provenance")))
}

// setupQuoteRemoval registers the built-in. No schedule: the standing row
// reaches it when a commit's removals arrive.
func (s *QNTXServer) setupQuoteRemoval() {
	s.daemon.Registry().Register(quote.NewRemoval(s.namespacePrompts, s.news,
		s.authHandler.MintedBy, s.logger.Named("quote.removal")))
}
