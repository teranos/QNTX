package auth

// "Tokens are to be removed from QNTX except for third-party OAuth clients"

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
	// 「[Opus 5.5] A grant am.toml lists is live while its line stands.」
	if t, listed := h.declaredGrant(did); listed {
		return t, true
	}
	if h.tokens == nil || did == "" {
		return TokenInfo{}, false
	}
	t, held := h.tokenOf(did)
	if !held || t.RevokedAt != nil || t.ExpiresAt == nil || expiredAt(t.ExpiresAt) {
		return TokenInfo{}, false
	}
	return t, true
}
