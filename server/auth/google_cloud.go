package auth

// "Given that I am ROOT and my google identity is the ROOT identity"
//
// ROOT connects the node to Google Cloud by saying yes once, at Google, on the
// OAuth client its Google login already uses: the node is handed a token on
// cloud-platform, spends it to set the federation up, and lets it go. Only
// ROOT's own Google account connects the node.

import (
	"context"
	"crypto/subtle"
	"html"
	"net/http"
	"time"

	errors "github.com/teranos/sacred-error"
)

// googleCloudPath is where ROOT is sent to Google to connect the node.
const googleCloudPath = "/auth/google/cloud"

// googleCloudScope is what the node asks ROOT for beyond the login: Google
// Cloud, which its setup is made in.
const googleCloudScope = "https://www.googleapis.com/auth/cloud-platform"

// GoogleCloudFix is a connecting stopped on one thing ROOT does at Google: what
// it is in a line, where to do it, and all that Google said.
type GoogleCloudFix struct {
	Says string
	Link string
	Full string
}

func (f *GoogleCloudFix) Error() string { return f.Full }

// GoogleCloudConnect sets the node up at Google Cloud on ROOT's token, in the
// project named or, named none, the one ROOT's account sees, and says what it set up.
type GoogleCloudConnect func(ctx context.Context, token, project, by string) (string, error)

// SetGoogleCloud is what connects the node once ROOT said yes.
func (h *Handler) SetGoogleCloud(connect GoogleCloudConnect) { h.googleCloud = connect }

// GoogleCloudPath is where ROOT connects the node, with the project as ?project=.
func GoogleCloudPath() string { return googleCloudPath }

// loginProject is the project an OAuth client was made in: its ID opens with the
// project's number. Empty when it does not.
func loginProject(clientID string) string {
	end := 0
	for end < len(clientID) && clientID[end] >= '0' && clientID[end] <= '9' {
		end++
	}
	if end == 0 || end == len(clientID) || clientID[end] != '-' {
		return ""
	}
	return clientID[:end]
}

// googleCloudAuthorize is Google's consent for the node's setup: the login's own
// grant, with cloud-platform added to it.
func googleCloudAuthorize(client OperatorClient, redirectURI string) string {
	return "https://" + googleAuthHost + "/o/oauth2/v2/auth" +
		"?client_id=" + urlEncode(client.ID) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&response_type=code" +
		"&scope=" + urlEncode("openid email "+googleCloudScope) +
		"&include_granted_scopes=true" +
		"&prompt=consent"
}

// handleGoogleCloud sends ROOT to Google to connect the node. Who said yes is
// read at the callback: only ROOT's Google account connects it.
func (h *Handler) handleGoogleCloud(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.google == nil {
		h.renderCeremonyPage(w, http.StatusNotFound, false,
			"This node has no Google login ([auth.provider.google]), whose consent connects it to Google Cloud")
		return
	}
	if h.googleCloud == nil {
		h.renderCeremonyPage(w, http.StatusServiceUnavailable, false, "This node has nothing that connects it to Google Cloud")
		return
	}
	ceremony, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ceremony ticket for Google Cloud", "error", err)
		h.renderCeremonyPage(w, http.StatusInternalServerError, false, "The ceremony ticket was not made")
		return
	}
	redirectURI := h.publicOrigin() + callbackPath
	state, err := h.bindingFlows.open(flow{
		provider:    "google",
		ceremony:    ceremony,
		state:       providerState{Host: googleAuthHost, ClientID: h.google.ID, ClientSecret: h.google.Secret},
		redirectURI: redirectURI,
		cloud:       true,
		project:     r.URL.Query().Get("project"),
	})
	if err != nil {
		h.renderCeremonyPage(w, http.StatusInternalServerError, false, "The ceremony was not recorded")
		return
	}
	h.setCeremonyCookie(w, ceremony)
	http.Redirect(w, r, googleCloudAuthorize(*h.google, redirectURI)+"&state="+urlEncode(state), http.StatusFound)
}

// googleCloudCallback is where Google returns ROOT's yes: the account is ROOT's
// or nothing is set up, and the token is the connector's to spend.
func (h *Handler) googleCloudCallback(w http.ResponseWriter, r *http.Request, fl flow, code string) {
	if subtle.ConstantTimeCompare([]byte(heldCeremony(r)), []byte(fl.ceremony)) != 1 {
		h.renderCeremonyPage(w, http.StatusForbidden, false,
			"This ceremony was started somewhere else. Start it from your own window.")
		return
	}
	acct, err := googleExchange(r.Context(), fl.state, code, fl.redirectURI)
	if err != nil {
		h.logger.Infow("Google Cloud was not connected: the exchange failed", "error", err)
		h.renderCeremonyPage(w, http.StatusUnauthorized, false, "The Google exchange did not complete: "+err.Error())
		return
	}
	if !h.IsRoot(acct.CanonicalID) {
		h.logger.Infow("Google Cloud was not connected: the account is not ROOT's", "canonical_id", acct.CanonicalID)
		h.renderCeremonyPage(w, http.StatusForbidden, false,
			acct.Handle+" is not ROOT's Google account, and only ROOT's connects the node to Google Cloud")
		return
	}
	if acct.google == "" {
		h.renderCeremonyPage(w, http.StatusBadGateway, false, errNoGoogleToken.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), googleCloudSetup)
	defer cancel()
	// "ROOT, who already set up the Google login in that project"
	project := fl.project
	if project == "" {
		project = loginProject(fl.state.ClientID)
	}
	said, err := h.googleCloud(ctx, acct.google, project, acct.CanonicalID)
	if err != nil {
		h.logger.Errorw("Google Cloud was not connected", "by", acct.CanonicalID, "project", fl.project, "error", err)
		h.renderCloudStopped(w, err, fl.project)
		return
	}
	h.logger.Infow("the node is connected to Google Cloud", "by", acct.CanonicalID, "said", said)
	h.renderCeremonyPage(w, http.StatusOK, true, said)
}

// renderCloudStopped says what stopped the node connecting: what is wrong, where
// to fix it when ROOT can, Try again, and all that was said.
// "THE BARE MINIMUM A USER NEEDS TO CONTINUE ESSENTIALLY"
func (h *Handler) renderCloudStopped(w http.ResponseWriter, err error, project string) {
	again := googleCloudPath
	if project != "" {
		again += "?project=" + urlEncode(project)
	}
	says, full, fixAt := "The node was not connected to Google Cloud: "+err.Error(), "", ""
	var fix *GoogleCloudFix
	if errors.As(err, &fix) {
		says, full, fixAt = fix.Says, fix.Full, fix.Link
	}
	const link = `style="display:inline-block;margin:0 8px 8px 0;padding:8px 12px;border:1px solid currentColor;color:#e6e4ef;text-decoration:none"`
	page := `<!doctype html><meta charset="utf-8"><title>QNTX</title>` +
		`<body style="background:#12111a;color:#e06060;font:14px ui-monospace,monospace;margin:0;padding:24px;word-break:break-word;overflow-wrap:break-word">` +
		`<p>` + html.EscapeString(says) + `</p><p>`
	if fixAt != "" {
		page += `<a ` + link + ` target="_blank" rel="noopener" href="` + html.EscapeString(fixAt) + `">1. Turn it on at Google</a>`
		page += `<a ` + link + ` href="` + html.EscapeString(again) + `">2. Try again</a>`
	} else {
		page += `<a ` + link + ` href="` + html.EscapeString(again) + `">Try again</a>`
	}
	page += `</p>`
	if full != "" {
		page += `<details style="color:#e6e4ef;opacity:.7"><summary>What Google said</summary>` + html.EscapeString(full) + `</details>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	if _, wrote := w.Write([]byte(page)); wrote != nil {
		h.logger.Infow("the Google Cloud page was not delivered", "error", wrote)
	}
}

// googleCloudSetup is how long the node takes to set itself up at Google Cloud:
// each step is an operation Google finishes on its own time.
const googleCloudSetup = 3 * time.Minute

// errNoGoogleToken is an exchange that named the account and handed no token.
var errNoGoogleToken = errors.New("Google handed no token to set the node up with")
