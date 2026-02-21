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
	"github.com/teranos/QNTX/errors"
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
	ceremonies sync.Map // ownerUserID -> *webauthn.SessionData
	logger     *zap.SugaredLogger
	corsWrap   func(http.HandlerFunc) http.HandlerFunc
}

// New creates an auth handler. corsWrap is the server's CORS middleware —
// auth routes need CORS headers but not auth checking.
func New(db *sql.DB, serverPort, frontendPort int, sessionExpiryHours int, logger *zap.SugaredLogger, corsWrap func(http.HandlerFunc) http.HandlerFunc) (*Handler, error) {
	origins := []string{
		fmt.Sprintf("http://localhost:%d", serverPort),
	}
	if frontendPort != serverPort {
		origins = append(origins, fmt.Sprintf("http://localhost:%d", frontendPort))
	}

	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "QNTX",
		RPID:          "localhost",
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create WebAuthn instance")
	}

	return &Handler{
		webauthn: w,
		creds:    newCredentialStore(db, logger),
		sessions: newSessionStore(sessionExpiryHours),
		logger:   logger,
		corsWrap: corsWrap,
	}, nil
}

// Middleware returns a handler wrapper that enforces authentication.
// API/WS requests without a valid session get 401.
// Page requests get redirected to /auth/login.
func (h *Handler) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
// These use CORS middleware but bypass auth middleware.
func (h *Handler) RegisterRoutes() {
	http.HandleFunc("/auth/login", h.corsWrap(h.handleLogin))
	http.HandleFunc("/auth/status", h.corsWrap(h.handleStatus))
	http.HandleFunc("/auth/register/begin", h.corsWrap(h.handleRegisterBegin))
	http.HandleFunc("/auth/register/finish", h.corsWrap(h.handleRegisterFinish))
	http.HandleFunc("/auth/login/begin", h.corsWrap(h.handleLoginBegin))
	http.HandleFunc("/auth/login/finish", h.corsWrap(h.handleLoginFinish))
	http.HandleFunc("/auth/logout", h.corsWrap(h.handleLogout))
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
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") || strings.HasPrefix(path, "/lsp") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
