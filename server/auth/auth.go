package auth

import (
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/errors"
	"go.uber.org/zap"

	_ "embed"
)

//go:embed auth_login.html
var loginHTML []byte

const sessionCookieName = "qntx_session"

// Handler provides WebAuthn authentication endpoints and middleware.
type Handler struct {
	webauthn       *webauthn.WebAuthn
	creds          *credentialStore
	sessions       *sessionStore
	layeChallenges layeChallenges
	bindingFlows   bindingFlows
	pendingLogins  pendingLogins
	// auth.root_identities and auth.binding_signers, re-read when am.toml
	// changes so revocation lands without a restart.
	identities identityLists
	nodeKey    ed25519.PrivateKey // the node DID key; this node signs bindings with it
	// auth.public_origin: where this node answers, which a ceremony's
	// redirect_uri is built from. Empty falls back to loopbackOrigin.
	configuredOrigin string
	// Where this node answers on the machine running it. A ceremony that has
	// been given no public origin can reach here and nowhere else.
	loopbackOrigin string
	signedBindings   sync.Map   // ceremony ticket -> the binding this node signed under it
	tokens           TokenStore // ADR-025: bearer token path; may be nil during init
	attestor         Attestor   // records admissions; nil until the store is up
	ceremonies       sync.Map   // ownerUserID -> *webauthn.SessionData
	secureCookies    bool       // set true when deployed behind TLS (non-loopback bind); www-readiness P1
	logger           *zap.SugaredLogger
	corsWrap         func(http.HandlerFunc) http.HandlerFunc
}

// New creates an auth handler. corsWrap is the server's CORS middleware —
// auth routes need CORS headers but not auth checking.
//
// rpID and rpOrigins come from [auth] rp_id / rp_origins in am.toml. Empty
// rpID falls back to "localhost"; empty rpOrigins falls back to loopback URLs
// derived from serverPort/frontendPort — local dev works with no config.
// server/init.go enforces that rpID must be set when bind_address is non-
// loopback and auth.enabled is true (browsers reject any WebAuthn ceremony
// whose RPID isn't a registrable domain suffix of the origin).
func New(db *sql.DB, rpID string, rpOrigins []string, serverPort, frontendPort int, sessionExpiryHours int, logger *zap.SugaredLogger, corsWrap func(http.HandlerFunc) http.HandlerFunc, tokens TokenStore, secureCookies bool, rootIdentities, bindingSigners []string) (*Handler, error) {
	if rpID == "" {
		rpID = "localhost"
	}
	if len(rpOrigins) == 0 {
		rpOrigins = []string{
			fmt.Sprintf("http://localhost:%d", serverPort),
		}
		if frontendPort != serverPort {
			rpOrigins = append(rpOrigins, fmt.Sprintf("http://localhost:%d", frontendPort))
		}
	}

	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "QNTX",
		RPID:          rpID,
		RPOrigins:     rpOrigins,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create WebAuthn instance")
	}

	h := &Handler{
		webauthn:       w,
		creds:          newCredentialStore(db, logger),
		sessions:       newSessionStore(sessionExpiryHours),
		tokens:         tokens,
		secureCookies:  secureCookies,
		loopbackOrigin: fmt.Sprintf("http://127.0.0.1:%d", serverPort),
		logger:         logger,
		corsWrap:       corsWrap,
	}
	h.SetIdentities(rootIdentities, bindingSigners)
	return h, nil
}

// SetIdentities replaces who may log in and whose bindings are trusted. The
// config watcher calls this, so striking an account out of am.toml revokes it
// and every device enrolled under it without waiting for a restart.
func (h *Handler) SetIdentities(rootIdentities, bindingSigners []string) {
	h.identities.set(rootIdentities, bindingSigners)
}

// SetPublicOrigin fixes the origin a provider redirects back to. Unset, it is
// read off the request, which believes X-Forwarded-Host — and whoever sets that
// header chooses where the authorization code is delivered.
func (h *Handler) SetPublicOrigin(origin string) {
	h.configuredOrigin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
}

// SetNodeKey hands the handler the node DID's private key, which is what it
// signs account bindings with.
func (h *Handler) SetNodeKey(key ed25519.PrivateKey) {
	h.nodeKey = key
}

// Middleware returns a handler wrapper that enforces authentication.
// API/WS requests without a valid session get 401.
// Page requests get redirected to /auth/login.
func (h *Handler) Middleware(next http.HandlerFunc) http.HandlerFunc {
	// TODO(#578): Verify user DID → node DID delegation instead of session cookie
	return func(w http.ResponseWriter, r *http.Request) {
		if h.tokens != nil {
			if raw, ok := bearerToken(r); ok {
				// The token names its own namespace, so this is where a request
				// is routed rather than defaulted.
				if grant, live := h.tokens.Lookup(sha256Hex(raw)); live {
					next(w, r.WithContext(WithCaller(r.Context(), Caller{
						Level:     LevelToken,
						Namespace: grant.Namespace,
						Identity:  grant.MintedBy,
						Grant:     &grant,
					})))
					return
				}
			}
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			h.rejectUnauthenticated(w, r)
			return
		}
		identity, ok := h.sessions.identityOf(cookie.Value)
		if !ok {
			h.rejectUnauthenticated(w, r)
			return
		}
		// am.toml is the only list of who SUPER is, so being on it is the check
		// (ADR-027). Handlers asked it one at a time before this; the level said
		// USER while the deployment meant otherwise.
		level := LevelAttestor
		if h.stillAdmitted(identity) {
			level = LevelSuper
		}
		next(w, r.WithContext(WithCaller(r.Context(), Caller{
			Level:     level,
			Namespace: NamespaceDefault,
			Identity:  identity,
		})))
	}
}

// RegisterRoutes registers all /auth/* routes on the default mux.
// Ceremony routes use CORS middleware but bypass auth middleware.
// Token management routes (ADR-025) require an authenticated passkey
// session — bearer tokens cannot mint new tokens.
func (h *Handler) RegisterRoutes() {
	http.HandleFunc("/auth/login", h.corsWrap(h.handleLogin))
	http.HandleFunc("/auth/status", h.corsWrap(h.handleStatus))
	http.HandleFunc("/auth/register/begin", h.corsWrap(h.handleRegisterBegin))
	http.HandleFunc("/auth/register/finish", h.corsWrap(h.handleRegisterFinish))
	http.HandleFunc("/auth/login/begin", h.corsWrap(h.handleLoginBegin))
	http.HandleFunc("/auth/login/finish", h.corsWrap(h.handleLoginFinish))
	http.HandleFunc("/auth/logout", h.corsWrap(h.handleLogout))
	// laye as an identity provider: it holds the key, the server checks a
	// signature over a challenge it issued.
	http.HandleFunc("/auth/laye/challenge", h.corsWrap(h.handleLayeChallenge))
	http.HandleFunc("/auth/laye/verify", h.corsWrap(h.handleLayeVerify))
	// The ceremony: the glyph asks what can be linked, starts one, and collects
	// the result. Everything the provider requires happens on this side of the
	// wire, so no page holds a secret and no page holds logic.
	http.HandleFunc("/auth/binding/providers", h.corsWrap(h.handleBindingProviders))
	http.HandleFunc("/auth/binding/start", h.corsWrap(h.handleBindingStart))
	http.HandleFunc(callbackPath, h.corsWrap(h.handleBindingCallback))
	http.HandleFunc("/auth/binding/result", h.corsWrap(h.handleBindingResult))
	// Cookie-gated so bearer tokens cannot mint or list tokens.
	http.HandleFunc("/auth/tokens", h.corsWrap(h.sessionOnly(h.tokensCollection)))
	http.HandleFunc("/auth/tokens/", h.corsWrap(h.sessionOnly(h.handleTokenByID)))
}

// tokensCollection dispatches on method for the /auth/tokens collection.
func (h *Handler) tokensCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateToken(w, r)
	case http.MethodGet:
		h.handleListTokens(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sessionOnly gates a handler on a valid passkey session cookie. Bearer
// tokens are rejected — ADR-025 forbids tokens from minting new tokens.
func (h *Handler) sessionOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !h.sessions.validate(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "passkey session required")
			return
		}
		next(w, r)
	}
}

// StartSessionSweep starts a background goroutine that cleans expired sessions
// every 5 minutes. Call done() from your WaitGroup, listen on cancel for shutdown.
func (h *Handler) StartSessionSweep(done func(), cancel <-chan struct{}) {
	go func() {
		defer done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.sessions.sweep()
				// Challenges, ceremonies and uncollected bindings are all
				// written by unauthenticated callers and expire on read, which
				// is never for anything abandoned.
				h.layeChallenges.sweep()
				h.bindingFlows.sweep()
				h.pendingLogins.sweep()
				h.sweepSignedBindings()
			case <-cancel:
				return
			}
		}
	}()
}

// rejectUnauthenticated answers in the caller's own terms: JSON for anything
// that parses JSON, a redirect to the login page for anything a person reads.
func (h *Handler) rejectUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
}

func isAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
