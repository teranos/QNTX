package server

import (
	"github.com/teranos/QNTX/server/quote"
	errors "github.com/teranos/sacred-error"
)

// namespacePrompts is every prompt a namespace holds.
func (s *QNTXServer) namespacePrompts(namespace string) ([]string, error) {
	if s.held == nil {
		return nil, errors.New("no namespaces are held to read prompts from")
	}
	return quote.PromptsIn(s.held, namespace)
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
