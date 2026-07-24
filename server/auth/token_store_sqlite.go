package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/teranos/errors"
	"go.uber.org/zap"
)

const tokenPrefix = "qntx_"

type sqliteTokenStore struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

func newSQLiteTokenStore(db *sql.DB, logger *zap.SugaredLogger) *sqliteTokenStore {
	return &sqliteTokenStore{db: db, logger: logger}
}

// NewSQLiteTokenStore is the exported constructor for use outside the package
// (e.g. server init wiring). Returns a *sqliteTokenStore which satisfies TokenStore.
func NewSQLiteTokenStore(db *sql.DB, logger *zap.SugaredLogger) TokenStore {
	return newSQLiteTokenStore(db, logger)
}

// Create issues a new access token. Returns the raw token (returned once,
// never stored) and the row id.
func (s *sqliteTokenStore) Create(label string, expiresAt *time.Time) (string, string, error) {
	raw, err := newRawToken()
	if err != nil {
		return "", "", err
	}
	id, err := newTokenID()
	if err != nil {
		return "", "", err
	}
	hash := sha256Hex(raw)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var exp any
	if expiresAt != nil {
		exp = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.Exec(
		`INSERT INTO access_tokens (id, label, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, label, hash, now, exp,
	)
	if err != nil {
		return "", "", errors.Wrapf(err, "insert access_token id=%s label=%s", id, label)
	}
	return raw, id, nil
}

// Lookup returns true when a matching token exists, is not revoked, and is
// not expired. Best-effort last_used_at bump.
func (s *sqliteTokenStore) Lookup(hash string) bool {
	var id string
	var expiresAt sql.NullString
	var revokedAt sql.NullString
	err := s.db.QueryRow(
		`SELECT id, expires_at, revoked_at FROM access_tokens WHERE token_hash = ?`,
		hash,
	).Scan(&id, &expiresAt, &revokedAt)
	if err != nil {
		return false
	}
	if revokedAt.Valid {
		return false
	}
	if expiresAt.Valid {
		exp, parseErr := time.Parse(time.RFC3339Nano, expiresAt.String)
		if parseErr == nil && time.Now().After(exp) {
			return false
		}
	}
	// Best-effort — a failure here doesn't invalidate the auth.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE access_tokens SET last_used_at = ? WHERE id = ?`, now, id); err != nil && s.logger != nil {
		s.logger.Debugw("failed to update last_used_at", "id", id, "error", err)
	}
	return true
}

// List returns all tokens, most recent first, without hashes.
func (s *sqliteTokenStore) List() ([]TokenInfo, error) {
	rows, err := s.db.Query(
		`SELECT id, label, created_at, expires_at, last_used_at, revoked_at
		 FROM access_tokens ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, errors.Wrap(err, "query access_tokens")
	}
	defer rows.Close()

	var out []TokenInfo
	for rows.Next() {
		var info TokenInfo
		var exp, used, rev sql.NullString
		if err := rows.Scan(&info.ID, &info.Label, &info.CreatedAt, &exp, &used, &rev); err != nil {
			return nil, errors.Wrap(err, "scan access_token row")
		}
		if exp.Valid {
			info.ExpiresAt = &exp.String
		}
		if used.Valid {
			info.LastUsedAt = &used.String
		}
		if rev.Valid {
			info.RevokedAt = &rev.String
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// Revoke marks a token revoked. Idempotent — repeated revoke is a no-op.
func (s *sqliteTokenStore) Revoke(id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`UPDATE access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return errors.Wrapf(err, "revoke access_token id=%s", id)
	}
	return nil
}

func newRawToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "generate access token random bytes")
	}
	return tokenPrefix + hex.EncodeToString(buf), nil
}

func newTokenID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "generate access token id")
	}
	return "AT_" + hex.EncodeToString(buf), nil
}
