package auth

import (
	"github.com/teranos/errors"
)

// RootAgentLabel is the ROOT agent's token by name: what the tokens list shows
// it as, and what a line names it by.
const RootAgentLabel = "root-agent"

// HoldNamespaceAgent writes down a namespace agent's token, when the node does
// not hold it yet (ADR-048): a token, acting in its namespace and nowhere else,
// speaking for whoever opted the namespace into it. Held once; a start or a
// first use after a restart finds it held.
func (h *Handler) HoldNamespaceAgent(raw, did, label, mintedBy, namespace string) error {
	if h.tokens == nil {
		return errors.New("this node keeps no tokens, so the agent has none to present at the gate")
	}
	hash := sha256Hex(raw)
	if grant, live, held := h.tokens.LookupSpent(hash); held {
		if !live {
			return errors.Newf("the agent's token %s is switched off", grant.ID)
		}
		if !h.stillAdmitted(grant.MintedBy) {
			return errors.Newf("the agent's token %s speaks for %s, which the node no longer admits", grant.ID, quoteIdentity(grant.MintedBy))
		}
		return nil
	}
	if mintedBy == "" {
		return errors.Newf("nobody opted %s into its agent, so there is nobody for its token to speak for", namespace)
	}
	// Derived from the node's key, so it is the same token for as long as the
	// namespace keeps its agent: it does not end.
	issued := IssuedToken{Hash: hash, DID: did, Label: label, MintedBy: mintedBy, Level: LevelToken, Namespaces: []string{namespace}, ExpiresAt: NeverEnds()}
	if h.users != nil {
		u, found, err := h.users.ByRoute(mintedBy)
		if err != nil {
			return errors.Wrapf(err, "the User %s reaches was not read, so the agent's token was not written", quoteIdentity(mintedBy))
		}
		if found {
			issued.MintedByUser, issued.MintedByDisplayName = u.ID, u.DisplayName
		}
	}
	id, err := h.tokens.Issue(issued)
	if err != nil {
		h.attest(PredicateUnanswered, mintedBy, map[string]any{
			"asked": "token store", "doing": "hold the agent of " + namespace, "error": err.Error(),
		})
		return errors.Wrapf(err, "the token of the agent of %s was not written", namespace)
	}
	h.attest(PredicateMinted, mintedBy, map[string]any{
		"token": id, "label": label, "level": string(LevelToken), "did": did, "namespace": namespace,
	})
	return nil
}

// HoldRootAgent writes down the ROOT agent's token, when the node does not
// hold it yet (ADR-048). The token is ROOT's kind, which minting hands to
// nobody: the node holds it for the one agent it runs as itself.
func (h *Handler) HoldRootAgent(raw, did string) error {
	if h.tokens == nil {
		return errors.New("this node keeps no tokens, so the ROOT agent has none to present at the gate")
	}
	hash := sha256Hex(raw)
	if grant, live, held := h.tokens.LookupSpent(hash); held {
		// Revocation is a switch, and it is ROOT's hand on it. A start does
		// not turn back on what ROOT turned off.
		if !live {
			return errors.Newf("the ROOT agent's token %s is switched off", grant.ID)
		}
		if !h.stillAdmitted(grant.MintedBy) {
			return errors.Newf("the ROOT agent's token %s speaks for %s, which auth.root_identities no longer lists", grant.ID, quoteIdentity(grant.MintedBy))
		}
		return nil
	}

	// A token speaks for whoever minted it (ADR-025), and this one speaks for
	// ROOT: every entry reaches the one User that is ROOT (ADR-031).
	roots := h.identities.roots()
	if len(roots) == 0 {
		return errors.New("auth.root_identities names nobody, so there is no ROOT for the ROOT agent to speak for")
	}
	issued := IssuedToken{Hash: hash, DID: did, Label: RootAgentLabel, MintedBy: roots[0], Level: LevelRoot, ExpiresAt: NeverEnds()}
	if h.users != nil {
		u, found, err := h.users.ByRoute(roots[0])
		if err != nil {
			return errors.Wrapf(err, "the User %s reaches was not read, so the ROOT agent's token was not written", quoteIdentity(roots[0]))
		}
		if found {
			issued.MintedByUser, issued.MintedByDisplayName = u.ID, u.DisplayName
		}
	}
	id, err := h.tokens.Issue(issued)
	if err != nil {
		h.attest(PredicateUnanswered, roots[0], map[string]any{
			"asked": "token store", "doing": "hold the ROOT agent", "error": err.Error(),
		})
		return errors.Wrap(err, "the ROOT agent's token was not written")
	}
	h.attest(PredicateMinted, roots[0], map[string]any{
		"token": id, "label": RootAgentLabel, "level": string(LevelRoot), "did": did,
	})
	return nil
}
