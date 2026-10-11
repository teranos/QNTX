package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// A peer's people log in at the primary (nodes.primary): the passkey is done
// there, and the code that comes back is exchanged here, signing as this
// node's did:key. The primary says who it is; this node's list says if they are in.

// elsewherePath is where this node's login sends somebody on their way.
const elsewherePath = "/auth/elsewhere"

// elsewhereBackPath is where the node they logged in at sends them back, and
// the return address this node is registered there with.
const elsewhereBackPath = "/auth/elsewhere/back"

// goneElsewhere is one person sent to log in at the other node: the PKCE
// verifier only this node holds, and where on this node they were going.
type goneElsewhere struct {
	verifier  string
	going     string
	startedAt time.Time
}

// elsewhereJourneys is everyone sent to log in elsewhere, by the state they carry.
type elsewhereJourneys struct {
	byState sync.Map
}

// SetPrimary names the primary node, or none: people log in there, and the
// tokens it issued are accepted here. A value that is not an absolute origin
// is refused rather than read as none.
func (h *Handler) SetPrimary(origin string) error {
	trimmed := strings.TrimSuffix(strings.TrimSpace(origin), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return errors.Wrapf(err, "nodes.primary %q does not parse as an origin", origin)
	}
	if !parsed.IsAbs() && names(trimmed) {
		return errors.Newf("nodes.primary %q is not an absolute origin such as https://api.example.com", origin)
	}
	h.loginAt = trimmed
	h.logsInElsewhere = parsed.IsAbs()
	h.issuers = h.issuers[:0]
	if parsed.IsAbs() {
		h.issuers = append(h.issuers, trimmed)
	}
	return nil
}

// goingOnThisNode is where somebody asked to go, kept to a path on this node.
// Anywhere else is the root.
func goingOnThisNode(asked string) string {
	if !strings.HasPrefix(asked, "/") || strings.HasPrefix(asked, "//") || strings.HasPrefix(asked, "/\\") {
		return "/"
	}
	return asked
}

// handleElsewhere sends somebody to log in at the node this one names, with a
// code challenge and a state only this node can answer.
func (h *Handler) handleElsewhere(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.logsInElsewhere {
		h.writeError(w, http.StatusNotFound, "this node's people log in here")
		return
	}
	did, err := h.ownDID()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "this node cannot name itself to "+h.loginAt+": "+err.Error())
		return
	}
	verifier, err := randomTicket()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no code verifier was drawn")
		return
	}
	state, err := randomTicket()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no state was drawn")
		return
	}
	now := time.Now()
	h.elsewhere.sweep(now)
	h.elsewhere.byState.Store(state, goneElsewhere{
		verifier:  verifier,
		going:     goingOnThisNode(r.URL.Query().Get("return")),
		startedAt: now,
	})
	challenge := sha256.Sum256([]byte(verifier))
	asked := url.Values{}
	asked.Set("client_id", did)
	asked.Set("redirect_uri", h.publicOrigin()+elsewhereBackPath)
	asked.Set("response_type", "code")
	asked.Set("state", state)
	asked.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	asked.Set("code_challenge_method", "S256")
	http.Redirect(w, r, h.loginAt+authorizePath+"?"+asked.Encode(), http.StatusFound)
}

// handleElsewhereBack is somebody sent back from logging in. The code is
// exchanged as this node, the other node says who the token is, and this
// node's list says whether they are in.
func (h *Handler) handleElsewhereBack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	asked := r.URL.Query()
	held, found := h.elsewhere.byState.LoadAndDelete(asked.Get("state"))
	gone, isGone := held.(goneElsewhere)
	if !found || !isGone || time.Since(gone.startedAt) > homewardTTL {
		h.writeError(w, http.StatusNotFound, "no journey for this state")
		return
	}
	if said := asked.Get("error"); names(said) {
		h.logger.Infow("The node logged in at refused somebody", "login_at", h.loginAt,
			"error", said, "description", asked.Get("error_description"))
		h.writeError(w, http.StatusUnauthorized, h.loginAt+" said "+said)
		return
	}
	now := time.Now()
	raw, err := h.exchangeElsewhere(asked.Get("code"), gone.verifier, now)
	if err != nil {
		h.attest(PredicateUnanswered, h.loginAt, map[string]any{"asked": h.loginAt, "doing": "exchange a code", "error": err.Error()})
		h.writeError(w, http.StatusBadGateway, "the code was not exchanged: "+err.Error())
		return
	}
	grant, live, err := h.askIssuer(h.loginAt, raw, now)
	if err != nil {
		h.attest(PredicateUnanswered, h.loginAt, map[string]any{"asked": h.loginAt, "doing": "say who logged in", "error": err.Error()})
		h.writeError(w, http.StatusBadGateway, h.loginAt+" did not say who logged in: "+err.Error())
		return
	}
	did, err := h.ownDID()
	if err != nil || !live || grant.ClientDID != did {
		h.writeError(w, http.StatusUnauthorized, h.loginAt+" vouches for nobody this node sent")
		return
	}
	if !h.stillAdmitted(grant.MintedBy) {
		h.logger.Infow("Somebody logged in elsewhere is not admitted here",
			"login_at", h.loginAt, "identity", quoteIdentity(grant.MintedBy))
		h.attest(PredicateRefused, grant.MintedBy, map[string]any{"login_at": h.loginAt, "reason": "not listed here"})
		h.writeError(w, http.StatusForbidden, "logged in at "+h.loginAt+", and not admitted here")
		return
	}
	token, err := h.sessions.create(grant.MintedBy, h.userFor(grant.MintedBy))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the session was not created: "+err.Error())
		return
	}
	h.setSessionCookie(w, token)
	h.attest(PredicateLoggedIn, grant.MintedBy, map[string]any{"provider": h.loginAt, "via": "login_at"})
	h.logger.Infow("Logged in elsewhere, a session here", "login_at", h.loginAt, "identity", quoteIdentity(grant.MintedBy))
	http.Redirect(w, r, gone.going, http.StatusSeeOther)
}

// exchangeElsewhere spends a code at the node logged in at, as this node, for
// the token it stands for.
func (h *Handler) exchangeElsewhere(code, verifier string, now time.Time) (string, error) {
	did, signed, err := h.asserted(h.loginAt, now)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", h.publicOrigin()+elsewhereBackPath)
	form.Set("code_verifier", verifier)
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	client := http.Client{Timeout: issuerTimeout}
	resp, err := client.PostForm(h.loginAt+tokenPath, form)
	if err != nil {
		return "", errors.Wrapf(err, "%s did not answer", h.loginAt+tokenPath)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return "", errors.Wrap(closeErr, "the token answer did not close")
	}
	if readErr != nil {
		return "", errors.Wrap(readErr, "the token answer did not read")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("%s answered %d: %s", h.loginAt+tokenPath, resp.StatusCode, body)
	}
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", errors.Wrapf(err, "the token answer is not a token: %s", body)
	}
	if !strings.HasPrefix(answer.AccessToken, tokenPrefix) {
		return "", errors.Newf("%s answered with no token", h.loginAt+tokenPath)
	}
	return answer.AccessToken, nil
}

// sweep drops journeys nobody came back from.
func (j *elsewhereJourneys) sweep(now time.Time) {
	j.byState.Range(func(state, held any) bool {
		gone, isGone := held.(goneElsewhere)
		if !isGone || now.Sub(gone.startedAt) > homewardTTL {
			j.byState.Delete(state)
		}
		return true
	})
}
