package server

// 「[Opus 5.5] The cap on each Session an element's agent runs in at Anthropic, ROOT's in」
// 「[Opus 5.5] Node Connectors (platform.claude.com/docs/en/managed-agents/budgets).」
// "make 6SION set one, prep a pr"
// "what  is max cap? like set it in the ui and make default of 200"

import (
	"context"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] The cap's unit is said here and nowhere else: ROOT gives it, the line keeps it and」
// 「[Opus 5.5] Node Connectors shows it in anthropicCapCurrency; Anthropic is handed it in that」
// 「[Opus 5.5] currency's cents (centsOf). USD is the only currency Anthropic caps a Session in.」
const (
	anthropicCapCurrency = "USD"
	// 「[Opus 5.5] anthropicDefaultCap is the cap of a node whose ROOT set none, in anthropicCapCurrency.」
	anthropicDefaultCap = "200"
)

// 「[Opus 5.5] centsOf is amount, in anthropicCapCurrency with at most two places after the point,」
// 「[Opus 5.5] as Anthropic takes a budget's amount: whole cents, digits only, no leading zero,」
// 「[Opus 5.5] above zero. Read as text, so no float rounds it.」
func centsOf(amount string) (string, error) {
	whole, part, pointed := strings.Cut(strings.TrimSpace(amount), ".")
	digits := func(s string) bool {
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	if whole == "" || !digits(whole) || !digits(part) || len(part) > 2 || (pointed && part == "") {
		return "", errors.Newf("%q is no amount of %s: a number above zero with at most two places after the point, 200 or 12.50", amount, anthropicCapCurrency)
	}
	cents := strings.TrimLeft(whole+part+strings.Repeat("0", 2-len(part)), "0")
	if cents == "" {
		return "", errors.Newf("a cap of %s %s caps nothing: Anthropic takes a cap above zero", amount, anthropicCapCurrency)
	}
	return cents, nil
}

// 「[Opus 5.5] capOf is the cap a Session is made with, in anthropicCapCurrency: ROOT's, or the default.」
func capOf(kept anthropicKept) (amount string, isDefault bool) {
	if kept.cap == "" {
		return anthropicDefaultCap, true
	}
	return kept.cap, false
}

// 「[Opus 5.5] anthropicCapSet keeps the cap ROOT gave on the node's ANTHROPIC line.」
func (s *QNTXServer) anthropicCapSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the cap on a Session at Anthropic is set through the gate, and this came through none"}
	}
	amount := strings.TrimSpace(sent["amount"])
	if _, err := centsOf(amount); err != nil {
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "amount", Says: err.Error()}
	}
	kept, _, err := s.anthropicLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept.cap = amount
	if refused := s.keepAnthropicLine(admitted.Identity, kept); refused != nil {
		return nil, refused
	}
	s.logger.Infow("the cap on a Session at Anthropic was set", "cap", amount, "currency", anthropicCapCurrency, "by", admitted.Identity)
	return s.anthropicAm(ctx, sent)
}
