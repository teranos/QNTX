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
// "2. fine change it, fix it"

// An agent has its own DID and its own credential (ADR-048), and an agent a
// token starts is handed one by the node: derived from the node's key the way
// its own agents' are, for the token it runs under and the agent it is
// (ADR-025, An agent a token starts). It is a route and no sigil, because a
// sigil is a tool to whoever reaches it, and a credential is offered to no
// model (ADR-048).

// agentTokenPath is where a token asks for the token of an agent it starts.
const agentTokenPath = "/auth/agent"

// agentOfLabel heads an agent's token by name, and the purpose it is derived
// for: agent-of:<the asking token's DID>:<agent>. The node writes it, and a
// session mints no token named so.
const agentOfLabel = "agent-of"

// agentLabel is the label and the purpose of the token of agent, started under
// the token whose DID is asking.
func agentLabel(asking, agent string) string {
	return agentOfLabel + ":" + asking + ":" + agent
}

// askedUnder is the DID of the token an agent's label says it runs under, and
// whether the label is an agent's at all. A did:key holds no colon past its
// own two, so the agent's part may hold any.
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

// handsAnAgent is the token an agent's token is handed to, or why this
// admission is none: a TOKEN or an ATTESTOR, minted rather than issued through
// a client, and no agent's own.
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
	// ROOT's kind is the node's own agents' (ADR-048), and SUPER is ROOT's
	// reach handed to a token (ADR-034): neither is handed on.
	return Grant{}, "a " + string(asking.Level) + " token is handed no agent's token: only a " + string(LevelToken) + " or an " + string(LevelAttestor) + " is"
}

// noAgent says why this names no agent: what a name is held to (i name).
func noAgent(agent string) string {
	switch {
	case agent == "" || strings.ContainsFunc(agent, unicode.IsControl):
		return "an agent is named by one line of text"
	case len(agent) > MaxName:
		return "the agent is named in more than " + strconv.Itoa(MaxName) + " characters"
	}
	return ""
}

// askingToken is the live token whose DID this is, and whether there is one:
// revoked, expired or gone is none.
func (h *Handler) askingToken(did string) (TokenInfo, bool) {
	if h.tokens == nil || did == "" {
		return TokenInfo{}, false
	}
	listed, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("could not list tokens, so no agent's token stands", "did", did, "error", err)
		return TokenInfo{}, false
	}
	for _, t := range listed {
		if t.DID != did {
			continue
		}
		if t.RevokedAt != nil || t.ExpiresAt == nil || expiredAt(t.ExpiresAt) {
			return TokenInfo{}, false
		}
		return t, true
	}
	return TokenInfo{}, false
}

// handleAgentToken hands the token asking the token of the agent it names.
// POST /auth/agent
// Body: {"agent": "<the agent>"}
// Response: {"token","did","label","agent","derived_by","level","namespaces","expires_at"}
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
	// The record of the token asking is when the agent's ends.
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
	handed := map[string]any{
		"token": raw, "did": did, "label": label, "agent": agent, "derived_by": asking.DID,
		"level": string(asking.Level), "namespaces": asking.Namespaces,
		"expires_at": ends.UTC().Format(time.RFC3339Nano),
	}
	// Held once: the same agent asking again is handed the same token.
	if held, live, isHeld := h.tokens.LookupSpent(hash); isHeld {
		// Revocation is a switch, and the hand on it holds: asking does not
		// turn back on what was turned off.
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
