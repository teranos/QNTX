package auth

import (
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
	webauthn   *webauthn.WebAuthn
	creds      *credentialStore
	sessions   *sessionStore
	tokens     TokenStore // ADR-025: bearer token path; may be nil during init
	ceremonies sync.Map   // ownerUserID -> *webauthn.SessionData
	logger     *zap.SugaredLogger
	corsWrap   func(http.HandlerFunc) http.HandlerFunc
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
func New(db *sql.DB, rpID string, rpOrigins []string, serverPort, frontendPort int, sessionExpiryHours int, logger *zap.SugaredLogger, corsWrap func(http.HandlerFunc) http.HandlerFunc, tokens TokenStore) (*Handler, error) {
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

	return &Handler{
		webauthn: w,
		creds:    newCredentialStore(db, logger),
		sessions: newSessionStore(sessionExpiryHours),
		tokens:   tokens,
		logger:   logger,
		corsWrap: corsWrap,
	}, nil
}

// Middleware returns a handler wrapper that enforces authentication.
// API/WS requests without a valid session get 401.
// Page requests get redirected to /auth/login.
func (h *Handler) Middleware(next http.HandlerFunc) http.HandlerFunc {
	// TODO(#578): Verify user DID → node DID delegation instead of session cookie
	return func(w http.ResponseWriter, r *http.Request) {
		if h.tokens != nil {
			if raw, ok := bearerToken(r); ok && h.tokens.Lookup(sha256Hex(raw)) {
				next(w, r)
				return
			}
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !h.sessions.validate(cookie.Value) {
			if isAPIRequest(r) {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			returnURL := r.URL.String()
			http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(returnURL), http.StatusSeeOther)
			return
		}
		next(w, r)
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
	// Cookie-gated so bearer tokens cannot mint or list tokens.
	http.HandleFunc("/auth/tokens", h.corsWrap(h.sessionOnly(h.tokensCollection)))
	http.HandleFunc("/auth/tokens/", h.corsWrap(h.sessionOnly(h.handleRevokeToken)))
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
			case <-cancel:
				return
			}
		}
	}()
}

func isAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
