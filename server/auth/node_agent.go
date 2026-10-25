package auth

import (
	errors "github.com/teranos/sacred-error"
)

// NodeAgentLabel is the Node Agent's token by name: what the tokens list shows
// it as, and what a line names it by.
const NodeAgentLabel = "root-agent"

// ElementAgentLabel heads an element agent's token by name: element-agent:<element> (ADR-054).
const ElementAgentLabel = "element-agent"

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
	u, found, err := h.users.ByRoute(mintedBy)
	if err != nil {
		return errors.Wrapf(err, "the User %s reaches was not read, so the agent's token was not written", quoteIdentity(mintedBy))
	}
	if found {
		issued.MintedByUser, issued.MintedByDisplayName = u.ID, u.DisplayName
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

// HoldNodeAgent writes down the Node Agent's token, when the node does not
// hold it yet (ADR-048). The token is ROOT's kind, which minting hands to
// nobody: the node holds it for the one agent it runs as itself.
func (h *Handler) HoldNodeAgent(raw, did string) error {
	return h.holdRootsAgent(raw, did, NodeAgentLabel, "the Node Agent")
}

// HoldElementAgent writes down an element agent's token, when the node does
// not hold it yet (ADR-054). It is ROOT's kind, as the Node Agent's is: the
// element is ROOT's to shake.
func (h *Handler) HoldElementAgent(raw, did, element string) error {
	return h.holdRootsAgent(raw, did, ElementAgentLabel+":"+element, "the element "+element)
}

// holdRootsAgent writes down the token of an agent that is ROOT's: labelled
// label, and called as a sentence names it.
func (h *Handler) holdRootsAgent(raw, did, label, called string) error {
	if h.tokens == nil {
		return errors.Newf("this node keeps no tokens, so %s has none to present at the gate", called)
	}
	hash := sha256Hex(raw)
	if grant, live, held := h.tokens.LookupSpent(hash); held {
		// Revocation is a switch, and it is ROOT's hand on it. A start does
		// not turn back on what ROOT turned off.
		if !live {
			return errors.Newf("%s's token %s is switched off", called, grant.ID)
		}
		if !h.stillAdmitted(grant.MintedBy) {
			return errors.Newf("%s's token %s speaks for %s, which auth.root_identities no longer lists", called, grant.ID, quoteIdentity(grant.MintedBy))
		}
		return nil
	}

	// A token speaks for whoever minted it (ADR-025), and this one speaks for
	// ROOT: every entry reaches the one User that is ROOT (ADR-031).
	roots := h.identities.roots()
	if len(roots) == 0 {
		return errors.Newf("auth.root_identities names nobody, so there is no ROOT for %s to speak for", called)
	}
	issued := IssuedToken{Hash: hash, DID: did, Label: label, MintedBy: roots[0], Level: LevelRoot, ExpiresAt: NeverEnds()}
	u, found, err := h.users.ByRoute(roots[0])
	if err != nil {
		return errors.Wrapf(err, "the User %s reaches was not read, so %s's token was not written", quoteIdentity(roots[0]), called)
	}
	if found {
		issued.MintedByUser, issued.MintedByDisplayName = u.ID, u.DisplayName
	}
	id, err := h.tokens.Issue(issued)
	if err != nil {
		h.attest(PredicateUnanswered, roots[0], map[string]any{
			"asked": "token store", "doing": "hold " + called, "error": err.Error(),
		})
		return errors.Wrapf(err, "%s's token was not written", called)
	}
	h.attest(PredicateMinted, roots[0], map[string]any{
		"token": id, "label": label, "level": string(LevelRoot), "did": did,
	})
	return nil
}
