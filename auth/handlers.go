package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Handlers provides HTTP handlers for authentication endpoints
type Handlers struct {
	service *Service
	store   *Store
	logger  *zap.SugaredLogger

	// PKCE state storage (in production, use Redis or similar)
	stateStore map[string]*oauthState
}

type oauthState struct {
	Provider    string
	CodeChallenge string
	CreatedAt   time.Time
	RedirectURI string
}

// NewHandlers creates new auth HTTP handlers
func NewHandlers(service *Service, store *Store, logger *zap.SugaredLogger) *Handlers {
	return &Handlers{
		service:    service,
		store:      store,
		logger:     logger,
		stateStore: make(map[string]*oauthState),
	}
}

// HandleProviders returns the list of enabled OAuth providers
// GET /auth/providers
func (h *Handlers) HandleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers := h.service.ListProviders()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": providers,
		"enabled":   h.service.Enabled(),
	})
}

// HandleAuthURL generates an OAuth authorization URL
// GET /auth/oauth/{provider}/url?redirect_uri=...
func (h *Handlers) HandleAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract provider from path (assumes /auth/oauth/{provider}/url format)
	providerName := extractProviderFromPath(r.URL.Path)
	if providerName == "" {
		http.Error(w, "missing provider", http.StatusBadRequest)
		return
	}

	provider, ok := h.service.GetProvider(providerName)
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	// Get optional code_challenge from client (for PKCE)
	codeChallenge := r.URL.Query().Get("code_challenge")
	redirectURI := r.URL.Query().Get("redirect_uri")

	// Generate state token
	state, err := generateState()
	if err != nil {
		h.logger.Errorw("Failed to generate state", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Store state for verification
	h.stateStore[state] = &oauthState{
		Provider:      providerName,
		CodeChallenge: codeChallenge,
		CreatedAt:     time.Now(),
		RedirectURI:   redirectURI,
	}

	// Clean up old states (simple cleanup, in production use TTL cache)
	h.cleanupOldStates()

	// Generate auth URL
	authURL := provider.AuthURL(state, codeChallenge)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"url":   authURL,
		"state": state,
	})
}

// HandleCallback handles the OAuth callback after user authorization
// POST /auth/oauth/callback
func (h *Handlers) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider     string `json:"provider"`
		Code         string `json:"code"`
		State        string `json:"state"`
		CodeVerifier string `json:"code_verifier"` // PKCE verifier
		DeviceID     string `json:"device_id"`
		DeviceName   string `json:"device_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Code == "" || req.State == "" || req.Provider == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	// Verify state
	storedState, ok := h.stateStore[req.State]
	if !ok {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}
	delete(h.stateStore, req.State) // One-time use

	// Verify provider matches
	if storedState.Provider != req.Provider {
		http.Error(w, "provider mismatch", http.StatusBadRequest)
		return
	}

	// Verify PKCE if code_challenge was used
	if storedState.CodeChallenge != "" && req.CodeVerifier != "" {
		if !verifyPKCE(req.CodeVerifier, storedState.CodeChallenge) {
			http.Error(w, "invalid code verifier", http.StatusBadRequest)
			return
		}
	}

	// Get provider
	provider, ok := h.service.GetProvider(req.Provider)
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens
	tokens, err := provider.Exchange(r.Context(), req.Code, req.CodeVerifier)
	if err != nil {
		h.logger.Errorw("Token exchange failed", "provider", req.Provider, "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	// Fetch user info
	userInfo, err := provider.UserInfo(r.Context(), tokens.AccessToken)
	if err != nil {
		h.logger.Errorw("Failed to fetch user info", "provider", req.Provider, "error", err)
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}

	// Get or create user
	user, err := h.store.GetOrCreateUser(r.Context(), req.Provider, userInfo)
	if err != nil {
		h.logger.Errorw("Failed to get/create user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate refresh token
	refreshToken, err := h.service.jwt.GenerateRefreshToken()
	if err != nil {
		h.logger.Errorw("Failed to generate refresh token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Create session
	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = "unknown"
	}
	deviceName := req.DeviceName
	if deviceName == "" {
		deviceName = "Unknown Device"
	}

	expiresAt := time.Now().Add(h.service.jwt.RefreshExpiry())
	session, err := h.store.CreateSession(r.Context(), user.ID, deviceID, deviceName, refreshToken, expiresAt)
	if err != nil {
		h.logger.Errorw("Failed to create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate JWT access token
	claims := &Claims{
		UserID:    user.ID,
		Email:     user.Email,
		SessionID: session.ID,
		DeviceID:  deviceID,
	}

	accessToken, err := h.service.jwt.GenerateToken(claims)
	if err != nil {
		h.logger.Errorw("Failed to generate access token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Infow("User authenticated",
		"user_id", user.ID,
		"email", user.Email,
		"provider", req.Provider,
		"device_id", deviceID,
	)

	// Return auth response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&AuthResponse{
		Token:        accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(h.service.jwt.TokenExpiry().Seconds()),
		User:         user,
	})
}

// HandleRefresh refreshes an expired access token using a refresh token
// POST /auth/refresh
func (h *Handlers) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.RefreshToken == "" {
		http.Error(w, "missing refresh token", http.StatusBadRequest)
		return
	}

	// Find session by refresh token
	session, err := h.store.GetSessionByRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		h.logger.Warnw("Failed to get session by refresh token", "error", err)
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}

	if session == nil {
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		http.Error(w, "refresh token expired", http.StatusUnauthorized)
		return
	}

	// Get user
	user, err := h.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		h.logger.Errorw("Failed to get user", "user_id", session.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate new access token
	claims := &Claims{
		UserID:    user.ID,
		Email:     user.Email,
		SessionID: session.ID,
		DeviceID:  session.DeviceID,
	}

	accessToken, err := h.service.jwt.GenerateToken(claims)
	if err != nil {
		h.logger.Errorw("Failed to generate access token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Debugw("Token refreshed", "user_id", user.ID, "session_id", session.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      accessToken,
		"expires_in": int(h.service.jwt.TokenExpiry().Seconds()),
	})
}

// HandleLogout revokes the current session
// POST /auth/logout
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.store.RevokeSession(r.Context(), claims.SessionID); err != nil {
		h.logger.Errorw("Failed to revoke session", "session_id", claims.SessionID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Infow("User logged out", "user_id", claims.UserID, "session_id", claims.SessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// HandleSessions lists active sessions for the current user
// GET /auth/sessions
func (h *Handlers) HandleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessions, err := h.store.ListUserSessions(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Errorw("Failed to list sessions", "user_id", claims.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Mark current session
	type sessionInfo struct {
		*Session
		Current bool `json:"current"`
	}

	result := make([]sessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = sessionInfo{
			Session: s,
			Current: s.ID == claims.SessionID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": result,
	})
}

// HandleRevokeSession revokes a specific session
// DELETE /auth/sessions/{id}
func (h *Handlers) HandleRevokeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from path
	sessionID := extractSessionIDFromPath(r.URL.Path)
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	// Verify session belongs to user
	session, err := h.store.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if session.UserID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := h.store.RevokeSession(r.Context(), sessionID); err != nil {
		h.logger.Errorw("Failed to revoke session", "session_id", sessionID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Infow("Session revoked", "user_id", claims.UserID, "revoked_session_id", sessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// HandleMe returns the current authenticated user info
// GET /auth/me
func (h *Handlers) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.store.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Errorw("Failed to get user", "user_id", claims.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// Helper functions

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func verifyPKCE(verifier, challenge string) bool {
	// S256: challenge = BASE64URL(SHA256(verifier))
	hash := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(hash[:])
	return computed == challenge
}

func (h *Handlers) cleanupOldStates() {
	// Remove states older than 10 minutes
	cutoff := time.Now().Add(-10 * time.Minute)
	for state, info := range h.stateStore {
		if info.CreatedAt.Before(cutoff) {
			delete(h.stateStore, state)
		}
	}
}

// extractProviderFromPath extracts provider name from path like /auth/oauth/github/url
func extractProviderFromPath(path string) string {
	// Expected format: /auth/oauth/{provider}/url
	parts := splitPath(path)
	for i, part := range parts {
		if part == "oauth" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// extractSessionIDFromPath extracts session ID from path like /auth/sessions/{id}
func extractSessionIDFromPath(path string) string {
	// Expected format: /auth/sessions/{id}
	parts := splitPath(path)
	for i, part := range parts {
		if part == "sessions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep rune) []string {
	return splitN(s, sep, -1)
}

func splitN(s string, sep rune, n int) []string {
	var parts []string
	start := 0
	for i, c := range s {
		if c == sep {
			if n > 0 && len(parts) >= n-1 {
				break
			}
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// hashForLogging creates a safe hash for logging tokens
func hashForLogging(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:8]) // First 8 bytes only
}
