package auth

import (
	"strings"

	"github.com/teranos/QNTX/internal/access"
)

// "Tokens are to be removed from QNTX except for third-party OAuth clients"

// 「[Opus 5.5] agentOfLabel heads an agent's token by name, and the purpose it is derived」
// 「[Opus 5.5] for: agent-of:<the asking token's DID>:<agent>. The node writes it, and a」
// 「[Opus 5.5] session mints no token named so.」
const agentOfLabel = "agent-of"

// 「[Opus 5.5] askedUnder is the DID of the token an agent's label says it runs under, and」
// 「[Opus 5.5] whether the label is an agent's at all. A did:key holds no colon past its」
// 「[Opus 5.5] own two, so the agent's part may hold any.」
func askedUnder(label string) (string, bool) {
	const didKey = "did:key:"
	rest, isAgents := strings.CutPrefix(label, agentOfLabel+":"+didKey)
	if !isAgents {
		return "", false
	}
	key, agent, named := strings.Cut(rest, ":")
	if !named || key == "" || agent == "" {
		return "", false
	}
	return didKey + key, true
}

// 「[Opus 5.5] derivedHere is whether this node's key derived the token with this DID」
// 「[Opus 5.5] under this label. A label is a name anybody minting could have written; the」
// 「[Opus 5.5] derivation is the node's alone.」
func (h *Handler) derivedHere(label, did string) bool {
	if h.nodeKey == nil {
		return false
	}
	_, derived, err := access.DeriveToken(h.nodeKey, label)
	return err == nil && derived == did
}

// 「[Opus 5.5] runsUnder is the token an agent's token runs under, whether this token is」
// 「[Opus 5.5] an agent's at all, and whether it stands: derived by this node's key under」
// 「[Opus 5.5] its own label, and the token it runs under live and a TOKEN or an ATTESTOR.」
// 「[Opus 5.5] One named as an agent's that does not stand is refused, not taken as the」
// 「[Opus 5.5] token its label names.」
func (h *Handler) runsUnder(label, did string) (asking TokenInfo, isAgents, stands bool) {
	under, isAgents := askedUnder(label)
	if !isAgents {
		return TokenInfo{}, false, false
	}
	if !h.derivedHere(label, did) {
		return TokenInfo{}, true, false
	}
	asking, live := h.askingToken(under)
	if !live || (asking.Level != LevelToken && asking.Level != LevelAttestor) {
		return TokenInfo{}, true, false
	}
	return asking, true, true
}

// 「[Opus 5.5] tokenByDID is a TokenStore that finds a token by its DID without listing」
// 「[Opus 5.5] every token. The operational table is one; a store that is not is listed.」
type tokenByDID interface {
	ByDID(did string) (TokenInfo, bool)
}

// 「[Opus 5.5] tokenOf is the token whose DID this is, revoked and expired ones included,」
// 「[Opus 5.5] and whether the store holds one.」
func (h *Handler) tokenOf(did string) (TokenInfo, bool) {
	if found, indexed := h.tokens.(tokenByDID); indexed {
		return found.ByDID(did)
	}
	listed, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("could not list tokens, so no agent's token stands", "did", did, "error", err)
		return TokenInfo{}, false
	}
	for _, t := range listed {
		if t.DID == did {
			return t, true
		}
	}
	return TokenInfo{}, false
}

// 「[Opus 5.5] askingToken is the live token whose DID this is, and whether there is one:」
// 「[Opus 5.5] revoked, expired or gone is none.」
func (h *Handler) askingToken(did string) (TokenInfo, bool) {
	if h.tokens == nil || did == "" {
		return TokenInfo{}, false
	}
	t, held := h.tokenOf(did)
	if !held || t.RevokedAt != nil || t.ExpiresAt == nil || expiredAt(t.ExpiresAt) {
		return TokenInfo{}, false
	}
	return t, true
}
