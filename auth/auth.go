// Package auth provides OAuth authentication for QNTX remote clients.
// It supports multiple OAuth providers (GitHub initially) and JWT-based sessions.
package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// User represents an authenticated user
type User struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`     // "github"
	ProviderID  string    `json:"provider_id"`  // ID from OAuth provider
	Email       string    `json:"email"`
	Name        string    `json:"name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
}

// Session represents an active login session
type Session struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	DeviceID     string    `json:"device_id"`
	DeviceName   string    `json:"device_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastActiveAt time.Time `json:"last_active_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// Claims represents JWT token claims for QNTX sessions
type Claims struct {
	UserID    string `json:"uid"`
	Email     string `json:"email"`
	SessionID string `json:"sid"`
	DeviceID  string `json:"did"`
}

// Provider defines the interface for OAuth providers
type Provider interface {
	// Name returns the provider identifier (e.g., "github")
	Name() string

	// AuthURL generates the OAuth authorization URL with PKCE support
	AuthURL(state, codeChallenge string) string

	// Exchange trades an authorization code for tokens
	Exchange(ctx context.Context, code, codeVerifier string) (*TokenResponse, error)

	// UserInfo fetches user details from the provider
	UserInfo(ctx context.Context, accessToken string) (*ProviderUserInfo, error)
}

// TokenResponse contains tokens from OAuth provider
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ProviderUserInfo contains user info from OAuth provider
type ProviderUserInfo struct {
	ProviderID string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
	Verified   bool   `json:"verified"`
}

// AuthResponse is returned after successful authentication
type AuthResponse struct {
	Token        string `json:"token"`         // JWT access token
	RefreshToken string `json:"refresh_token"` // Long-lived refresh token
	ExpiresIn    int    `json:"expires_in"`    // Seconds until token expires
	User         *User  `json:"user"`
}

// Service handles authentication operations
type Service struct {
	db        *sql.DB
	config    *am.AuthConfig
	jwt       *JWTManager
	providers map[string]Provider
	logger    *zap.SugaredLogger
}

// NewService creates a new auth service
func NewService(db *sql.DB, config *am.AuthConfig, logger *zap.SugaredLogger) (*Service, error) {
	if !config.Enabled {
		return nil, nil // Auth disabled, return nil service
	}

	// Initialize JWT manager
	jwt, err := NewJWTManager(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize JWT manager")
	}

	s := &Service{
		db:        db,
		config:    config,
		jwt:       jwt,
		providers: make(map[string]Provider),
		logger:    logger,
	}

	// Register enabled providers
	if config.GitHub.Enabled {
		if config.GitHub.ClientID == "" || config.GitHub.ClientSecret == "" {
			return nil, errors.New("GitHub OAuth enabled but client_id or client_secret not configured")
		}
		s.providers["github"] = NewGitHubProvider(&config.GitHub)
		logger.Infow("GitHub OAuth provider registered")
	}

	if len(s.providers) == 0 {
		return nil, errors.New("auth enabled but no OAuth providers configured")
	}

	return s, nil
}

// GetProvider returns a registered OAuth provider
func (s *Service) GetProvider(name string) (Provider, bool) {
	p, ok := s.providers[name]
	return p, ok
}

// ListProviders returns names of enabled OAuth providers
func (s *Service) ListProviders() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}

// Enabled returns whether authentication is enabled
func (s *Service) Enabled() bool {
	return s != nil && s.config != nil && s.config.Enabled
}

// GetJWT returns the JWT manager for token operations
func (s *Service) GetJWT() *JWTManager {
	return s.jwt
}
