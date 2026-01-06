package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/errors"
)

// Store handles persistence of users and sessions
type Store struct {
	db *sql.DB
}

// NewStore creates a new auth store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetOrCreateUser finds an existing user by provider ID or creates a new one
func (s *Store) GetOrCreateUser(ctx context.Context, provider string, info *ProviderUserInfo) (*User, error) {
	// Try to find existing user
	user, err := s.GetUserByProvider(ctx, provider, info.ProviderID)
	if err == nil {
		// Update last login time
		_, updateErr := s.db.ExecContext(ctx,
			"UPDATE users SET last_login_at = ?, name = ?, avatar_url = ? WHERE id = ?",
			time.Now(), info.Name, info.AvatarURL, user.ID,
		)
		if updateErr != nil {
			return nil, errors.Wrap(updateErr, "failed to update user last login")
		}
		user.LastLoginAt = time.Now()
		user.Name = info.Name
		user.AvatarURL = info.AvatarURL
		return user, nil
	}

	// Create new user
	user = &User{
		ID:          uuid.New().String(),
		Provider:    provider,
		ProviderID:  info.ProviderID,
		Email:       info.Email,
		Name:        info.Name,
		AvatarURL:   info.AvatarURL,
		CreatedAt:   time.Now(),
		LastLoginAt: time.Now(),
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, provider, provider_id, email, name, avatar_url, created_at, last_login_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Provider, user.ProviderID, user.Email, user.Name, user.AvatarURL, user.CreatedAt, user.LastLoginAt,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create user")
	}

	return user, nil
}

// GetUserByProvider finds a user by OAuth provider and provider ID
func (s *Store) GetUserByProvider(ctx context.Context, provider, providerID string) (*User, error) {
	user := &User{}
	var lastLogin sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider, provider_id, email, name, avatar_url, created_at, last_login_at
		 FROM users WHERE provider = ? AND provider_id = ?`,
		provider, providerID,
	).Scan(&user.ID, &user.Provider, &user.ProviderID, &user.Email, &user.Name, &user.AvatarURL, &user.CreatedAt, &lastLogin)

	if err == sql.ErrNoRows {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user")
	}

	if lastLogin.Valid {
		user.LastLoginAt = lastLogin.Time
	}

	return user, nil
}

// GetUserByID finds a user by internal ID
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	user := &User{}
	var lastLogin sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider, provider_id, email, name, avatar_url, created_at, last_login_at
		 FROM users WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.Provider, &user.ProviderID, &user.Email, &user.Name, &user.AvatarURL, &user.CreatedAt, &lastLogin)

	if err == sql.ErrNoRows {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user")
	}

	if lastLogin.Valid {
		user.LastLoginAt = lastLogin.Time
	}

	return user, nil
}

// CreateSession creates a new session for a user
func (s *Store) CreateSession(ctx context.Context, userID, deviceID, deviceName, refreshToken string, expiresAt time.Time) (*Session, error) {
	session := &Session{
		ID:           uuid.New().String(),
		UserID:       userID,
		DeviceID:     deviceID,
		DeviceName:   deviceName,
		CreatedAt:    time.Now(),
		ExpiresAt:    expiresAt,
		LastActiveAt: time.Now(),
	}

	// Store hashed refresh token
	tokenHash := hashToken(refreshToken)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, device_id, device_name, refresh_token_hash, created_at, expires_at, last_active_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.DeviceID, session.DeviceName, tokenHash, session.CreatedAt, session.ExpiresAt, session.LastActiveAt,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create session")
	}

	return session, nil
}

// GetSession retrieves a session by ID
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	session := &Session{}
	var lastActive, revoked sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, device_id, device_name, created_at, expires_at, last_active_at, revoked_at
		 FROM sessions WHERE id = ?`,
		id,
	).Scan(&session.ID, &session.UserID, &session.DeviceID, &session.DeviceName, &session.CreatedAt, &session.ExpiresAt, &lastActive, &revoked)

	if err == sql.ErrNoRows {
		return nil, nil // Session not found
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get session")
	}

	if lastActive.Valid {
		session.LastActiveAt = lastActive.Time
	}
	if revoked.Valid {
		session.RevokedAt = &revoked.Time
	}

	return session, nil
}

// GetSessionByRefreshToken finds a session by refresh token
func (s *Store) GetSessionByRefreshToken(ctx context.Context, refreshToken string) (*Session, error) {
	tokenHash := hashToken(refreshToken)
	session := &Session{}
	var lastActive, revoked sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, device_id, device_name, created_at, expires_at, last_active_at, revoked_at
		 FROM sessions WHERE refresh_token_hash = ? AND revoked_at IS NULL`,
		tokenHash,
	).Scan(&session.ID, &session.UserID, &session.DeviceID, &session.DeviceName, &session.CreatedAt, &session.ExpiresAt, &lastActive, &revoked)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get session by refresh token")
	}

	if lastActive.Valid {
		session.LastActiveAt = lastActive.Time
	}
	if revoked.Valid {
		session.RevokedAt = &revoked.Time
	}

	return session, nil
}

// UpdateSessionActivity updates the last active time for a session
func (s *Store) UpdateSessionActivity(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET last_active_at = ? WHERE id = ?",
		time.Now(), sessionID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to update session activity")
	}
	return nil
}

// RevokeSession marks a session as revoked
func (s *Store) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET revoked_at = ? WHERE id = ?",
		time.Now(), sessionID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to revoke session")
	}
	return nil
}

// RevokeAllUserSessions revokes all sessions for a user
func (s *Store) RevokeAllUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL",
		time.Now(), userID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to revoke user sessions")
	}
	return nil
}

// ListUserSessions returns all active sessions for a user
func (s *Store) ListUserSessions(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, device_id, device_name, created_at, expires_at, last_active_at, revoked_at
		 FROM sessions WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		 ORDER BY last_active_at DESC`,
		userID, time.Now(),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user sessions")
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		var lastActive, revoked sql.NullTime

		err := rows.Scan(&session.ID, &session.UserID, &session.DeviceID, &session.DeviceName,
			&session.CreatedAt, &session.ExpiresAt, &lastActive, &revoked)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan session")
		}

		if lastActive.Valid {
			session.LastActiveAt = lastActive.Time
		}
		if revoked.Valid {
			session.RevokedAt = &revoked.Time
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// CleanupExpiredSessions removes expired sessions from the database
func (s *Store) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE expires_at < ?",
		time.Now(),
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to cleanup expired sessions")
	}
	return result.RowsAffected()
}

// hashToken creates a SHA-256 hash of a token for secure storage
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
