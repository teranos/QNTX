package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// UserContextKey is the context key for the authenticated user claims
	UserContextKey contextKey = "auth_user"
)

// Middleware provides HTTP authentication middleware
type Middleware struct {
	service *Service
	store   *Store
	logger  *zap.SugaredLogger

	// Activity update debouncing
	activityMu     sync.Mutex
	lastActivity   map[string]time.Time
	activityWindow time.Duration
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(service *Service, store *Store, logger *zap.SugaredLogger) *Middleware {
	return &Middleware{
		service:        service,
		store:          store,
		logger:         logger,
		lastActivity:   make(map[string]time.Time),
		activityWindow: 5 * time.Minute, // Only update activity every 5 minutes per session
	}
}

// RequireAuth is middleware that requires a valid JWT token
// If auth is disabled globally, it passes through all requests
func (m *Middleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If auth service is nil (disabled), pass through
		if m.service == nil || !m.service.Enabled() {
			next(w, r)
			return
		}

		// Extract token from Authorization header or query param
		token := extractToken(r)
		if token == "" {
			http.Error(w, "unauthorized: missing token", http.StatusUnauthorized)
			return
		}

		// Validate JWT
		claims, err := m.service.jwt.ValidateToken(token)
		if err != nil {
			m.logger.Debugw("Token validation failed", "error", err)
			http.Error(w, "unauthorized: invalid token", http.StatusUnauthorized)
			return
		}

		// Verify session is still valid (not revoked)
		session, err := m.store.GetSession(r.Context(), claims.SessionID)
		if err != nil {
			m.logger.Warnw("Failed to get session", "session_id", claims.SessionID, "error", err)
			http.Error(w, "unauthorized: session error", http.StatusUnauthorized)
			return
		}

		if session == nil {
			http.Error(w, "unauthorized: session not found", http.StatusUnauthorized)
			return
		}

		if session.RevokedAt != nil {
			http.Error(w, "unauthorized: session revoked", http.StatusUnauthorized)
			return
		}

		if time.Now().After(session.ExpiresAt) {
			http.Error(w, "unauthorized: session expired", http.StatusUnauthorized)
			return
		}

		// Update session activity (debounced)
		m.touchSession(r.Context(), claims.SessionID)

		// Add claims to request context
		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// OptionalAuth is middleware that validates auth if present, but doesn't require it
// Useful for endpoints that behave differently for authenticated vs anonymous users
func (m *Middleware) OptionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If auth service is nil (disabled), pass through
		if m.service == nil || !m.service.Enabled() {
			next(w, r)
			return
		}

		// Try to extract and validate token
		token := extractToken(r)
		if token != "" {
			claims, err := m.service.jwt.ValidateToken(token)
			if err == nil {
				// Verify session
				session, _ := m.store.GetSession(r.Context(), claims.SessionID)
				if session != nil && session.RevokedAt == nil && time.Now().Before(session.ExpiresAt) {
					m.touchSession(r.Context(), claims.SessionID)
					ctx := context.WithValue(r.Context(), UserContextKey, claims)
					r = r.WithContext(ctx)
				}
			}
		}

		next(w, r)
	}
}

// touchSession updates session activity with debouncing
func (m *Middleware) touchSession(ctx context.Context, sessionID string) {
	m.activityMu.Lock()
	defer m.activityMu.Unlock()

	lastUpdate, ok := m.lastActivity[sessionID]
	if ok && time.Since(lastUpdate) < m.activityWindow {
		return // Skip update, too recent
	}

	m.lastActivity[sessionID] = time.Now()

	// Update in background to not block request
	go func() {
		if err := m.store.UpdateSessionActivity(context.Background(), sessionID); err != nil {
			m.logger.Warnw("Failed to update session activity", "session_id", sessionID, "error", err)
		}
	}()
}

// extractToken extracts the JWT token from request
// Checks Authorization header first, then falls back to query param (for WebSocket)
func extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// Support "Bearer <token>" format
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// Fallback to query param (for WebSocket connections)
	return r.URL.Query().Get("token")
}

// UserFromContext extracts authenticated user claims from request context
func UserFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(UserContextKey).(*Claims)
	return claims
}

// IsAuthenticated checks if the request has valid authentication
func IsAuthenticated(ctx context.Context) bool {
	return UserFromContext(ctx) != nil
}
