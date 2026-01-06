package auth

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
)

// JWTClaims extends standard JWT claims with QNTX-specific fields
type JWTClaims struct {
	jwt.RegisteredClaims
	UserID    string `json:"uid"`
	Email     string `json:"email"`
	SessionID string `json:"sid"`
	DeviceID  string `json:"did"`
}

// JWTManager handles JWT token creation and validation
type JWTManager struct {
	secret        []byte
	tokenExpiry   time.Duration
	refreshExpiry time.Duration
}

// NewJWTManager creates a new JWT manager with the given configuration
func NewJWTManager(config *am.AuthConfig) (*JWTManager, error) {
	secret := config.JWTSecret
	if secret == "" {
		// Auto-generate a secure secret if not provided
		generated, err := generateSecureSecret(32)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate JWT secret")
		}
		secret = generated
	}

	tokenExpiry, err := time.ParseDuration(config.TokenExpiry)
	if err != nil {
		tokenExpiry = 15 * time.Minute // Default
	}

	refreshExpiry, err := time.ParseDuration(config.RefreshExpiry)
	if err != nil {
		refreshExpiry = 30 * 24 * time.Hour // Default 30 days
	}

	return &JWTManager{
		secret:        []byte(secret),
		tokenExpiry:   tokenExpiry,
		refreshExpiry: refreshExpiry,
	}, nil
}

// GenerateToken creates a new JWT access token for the given claims
func (m *JWTManager) GenerateToken(claims *Claims) (string, error) {
	now := time.Now()
	jwtClaims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.tokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "qntx",
		},
		UserID:    claims.UserID,
		Email:     claims.Email,
		SessionID: claims.SessionID,
		DeviceID:  claims.DeviceID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	return token.SignedString(m.secret)
}

// ValidateToken parses and validates a JWT token, returning the claims
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Newf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secret, nil
	})

	if err != nil {
		return nil, errors.Wrap(err, "invalid token")
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return &Claims{
			UserID:    claims.UserID,
			Email:     claims.Email,
			SessionID: claims.SessionID,
			DeviceID:  claims.DeviceID,
		}, nil
	}

	return nil, errors.New("invalid token claims")
}

// GenerateRefreshToken creates a secure random refresh token
func (m *JWTManager) GenerateRefreshToken() (string, error) {
	return generateSecureSecret(32)
}

// TokenExpiry returns the configured token expiry duration
func (m *JWTManager) TokenExpiry() time.Duration {
	return m.tokenExpiry
}

// RefreshExpiry returns the configured refresh token expiry duration
func (m *JWTManager) RefreshExpiry() time.Duration {
	return m.refreshExpiry
}

// generateSecureSecret generates a cryptographically secure random hex string
func generateSecureSecret(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "failed to generate random bytes")
	}
	return hex.EncodeToString(b), nil
}
