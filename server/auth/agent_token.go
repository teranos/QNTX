package auth

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/teranos/QNTX/internal/access"
	errors "github.com/teranos/sacred-error"
)

// "give yourself a name, and after you have given yourslef a name, ask each agent to give themselves a name."
//
// "「[Opus 5.5] 2. fine change it, fix it」"

// 「[Opus 5.5] An agent has its own DID and its own credential (ADR-048), and an agent a」
// 「[Opus 5.5] token starts is handed one by the node: derived from the node's key the way」
// 「[Opus 5.5] its own agents' are, for the token it runs under and the agent it is」
// 「[Opus 5.5] (ADR-025, An agent a token starts). It is a route and no sigil, because a」
// 「[Opus 5.5] sigil is a tool to whoever reaches it, and a credential is offered to no」
// 「[Opus 5.5] model (ADR-048).」

// 「[Opus 5.5] agentTokenPath is where a token asks for the token of an agent it starts.」
const agentTokenPath = "/auth/agent"

// 「[Opus 5.5] agentOfLabel heads an agent's token by name, and the purpose it is derived」
// 「[Opus 5.5] for: agent-of:<the asking token's DID>:<agent>. The node writes it, and a」
// 「[Opus 5.5] session mints no token named so.」
const agentOfLabel = "agent-of"

// 「[Opus 5.5] agentLabel is the label and the purpose of the token of agent, started under」
// 「[Opus 5.5] the token whose DID is asking.」
func agentLabel(asking, agent string) string {
	return agentOfLabel + ":" + asking + ":" + agent
}

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

// 「[Opus 5.5] handsAnAgent is the token an agent's token is handed to, or why this」
// 「[Opus 5.5] admission is none: a TOKEN or an ATTESTOR, minted rather than issued through」
// 「[Opus 5.5] a client, and no agent's own.」
func handsAnAgent(admitted Admission) (Grant, string) {
	if admitted.Grant == nil {
		return Grant{}, "an agent's token is handed to the token it runs under, and this asks as a person, who mints one at /auth/tokens"
	}
	asking := *admitted.Grant
	if asking.ClientDID != "" || admitted.ClientDID != "" {
		return Grant{}, "this token was issued through the client " + asking.ClientDID + ", which is issued its next one under another DID: an agent derived from it would not last"
	}
	if _, isAgents := askedUnder(asking.Label); isAgents {
		return Grant{}, "this is an agent's own token, and an agent's token hands out none"
	}
	switch asking.Level {
	case LevelToken, LevelAttestor:
		return asking, ""
	}
	// 「[Opus 5.5] ROOT's kind is the node's own agents' (ADR-048), and SUPER is ROOT's」
	// 「[Opus 5.5] reach handed to a token (ADR-034): neither is handed on.」
	return Grant{}, "a " + string(asking.Level) + " token is handed no agent's token: only a " + string(LevelToken) + " or an " + string(LevelAttestor) + " is"
}

// 「[Opus 5.5] noAgent says why this names no agent: what a name is held to (i name).」
func noAgent(agent string) string {
	switch {
	case agent == "" || strings.ContainsFunc(agent, unicode.IsControl):
		return "an agent is named by one line of text"
	case len(agent) > MaxName:
		return "the agent is named in more than " + strconv.Itoa(MaxName) + " characters"
	}
	return ""
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

// 「[Opus 5.5] handleAgentToken hands the token asking the token of the agent it names.」
// 「[Opus 5.5] POST /auth/agent」
// 「[Opus 5.5] Body: {"agent": "<the agent>"}」
// 「[Opus 5.5] Response: {"token","did","label","agent","derived_by","level","namespaces","expires_at"}」
func (h *Handler) handleAgentToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no tokens, so an agent has none to be handed")
		return
	}
	if h.nodeKey == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node has no key of its own to derive an agent's token from")
		return
	}
	admitted, ok := AdmissionFrom(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "this request carries no admission")
		return
	}
	asking, why := handsAnAgent(admitted)
	if why != "" {
		h.writeError(w, http.StatusForbidden, why)
		return
	}
	var req struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON: "+err.Error())
		return
	}
	agent := strings.TrimSpace(req.Agent)
	if says := noAgent(agent); says != "" {
		h.writeError(w, http.StatusBadRequest, says)
		return
	}
	// 「[Opus 5.5] The record of the token asking is when the agent's ends.」
	record, live := h.askingToken(asking.DID)
	if !live {
		h.writeError(w, http.StatusForbidden, "the token asking is not one the node holds live")
		return
	}
	ends, err := time.Parse(time.RFC3339Nano, *record.ExpiresAt)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "when the token asking ends does not read: "+err.Error())
		return
	}

	label := agentLabel(asking.DID, agent)
	raw, did, err := access.DeriveToken(h.nodeKey, label)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the token of "+agent+" was not derived: "+err.Error())
		return
	}
	hash := sha256Hex(raw)
	// 「[Opus 5.5] Looked up and written as one: two asks that both found it absent would」
	// 「[Opus 5.5] both write it, and the second write would turn back on a token switched」
	// 「[Opus 5.5] off in between. One process only, as creating a User is.」
	h.handingAgents.Lock()
	defer h.handingAgents.Unlock()
	handed := map[string]any{
		"token": raw, "did": did, "label": label, "agent": agent, "derived_by": asking.DID,
		"level": string(asking.Level), "namespaces": asking.Namespaces,
		"expires_at": ends.UTC().Format(time.RFC3339Nano),
	}
	// 「[Opus 5.5] Held once: the same agent asking again is handed the same token.」
	if held, live, isHeld := h.tokens.LookupSpent(hash); isHeld {
		// 「[Opus 5.5] Revocation is a switch, and the hand on it holds: asking does not」
		// 「[Opus 5.5] turn back on what was turned off.」
		if !live {
			h.writeError(w, http.StatusForbidden, "the token of "+agent+", "+held.ID+", is switched off")
			return
		}
		h.writeJSON(w, http.StatusOK, handed)
		return
	}
	id, err := h.tokens.Issue(IssuedToken{
		Hash: hash, DID: did, Label: label,
		MintedBy: asking.MintedBy, MintedByUser: asking.MintedByUser, MintedByDisplayName: asking.MintedByDisplayName,
		Level: asking.Level, Namespaces: asking.Namespaces, ExpiresAt: &ends,
	})
	if err != nil {
		h.attest(PredicateUnanswered, asking.MintedBy, map[string]any{
			"asked": "token store", "doing": "hand " + agent + " its token", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, errors.Wrapf(err, "the token of %s was not written", agent).Error())
		return
	}
	h.attest(PredicateMinted, asking.MintedBy, map[string]any{
		"token": id, "label": label, "level": string(asking.Level), "did": did, "namespaces": asking.Namespaces,
		"derived_by": asking.DID, "agent": agent,
	})
	h.logger.Infow("an agent was handed a token of its own", "did", did, "agent", agent, "derived_by", asking.DID)
	h.writeJSON(w, http.StatusCreated, handed)
}
