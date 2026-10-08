package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sync"
	"time"

	"github.com/teranos/errors"
	"go.uber.org/zap"
)

type session struct {
	token string
	// What admitted this session: an entry from auth.root_identities. Empty
	// when nothing in this deployment names an identity, which is the state a
	// passkey-only install stays in.
	identity string
	// Who that identity reaches (ADR-031), resolved once here rather than on
	// every request. Requests outnumber logins, and the User store is a scan.
	userID      string
	displayName string
	// The door that User registered at, which is the namespace they act in
	// (ADR-032). Empty for a User that walked up to no door — ROOT, and
	// everyone somebody else put here.
	namespace string

	expiresAt time.Time
}

// "I'm pretty sure i logged in 5 times today with no additional security benifit to speak of"

// sessionStore holds sessions in the process and, given a db, in the
// operational db too, so a restart signs nobody out.
type sessionStore struct {
	sessions sync.Map
	expiry   time.Duration
	db       *sql.DB
	logger   *zap.SugaredLogger
}

func newSessionStore(expiryHours int) *sessionStore {
	return &sessionStore{
		expiry: time.Duration(expiryHours) * time.Hour,
		logger: zap.NewNop().Sugar(),
	}
}

// kept is a session store that also keeps its sessions in db.
func (s *sessionStore) kept(db *sql.DB, logger *zap.SugaredLogger) *sessionStore {
	s.db, s.logger = db, logger
	return s
}

// hashOf is how a token is named in the db.
func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// load is the session a token names: the process's, or the db's from before a
// restart.
func (s *sessionStore) load(token string) (*session, bool) {
	if val, ok := s.sessions.Load(token); ok {
		sess, isSession := val.(*session)
		return sess, isSession
	}
	if s.db == nil || token == "" {
		return nil, false
	}
	sess := &session{token: token}
	var expires int64
	err := s.db.QueryRow(`SELECT identity, user_id, display_name, namespace, expires_at FROM auth_sessions WHERE hash = ?`, hashOf(token)).
		Scan(&sess.identity, &sess.userID, &sess.displayName, &sess.namespace, &expires)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.logger.Warnw("a session was not read from the db", "error", err)
		}
		return nil, false
	}
	sess.expiresAt = time.Unix(expires, 0)
	s.sessions.Store(token, sess)
	return sess, true
}

// forget removes a session from the db; the process's copy is the caller's.
func (s *sessionStore) forget(token string) {
	if s.db == nil {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM auth_sessions WHERE hash = ?`, hashOf(token)); err != nil {
		s.logger.Warnw("a session was not removed from the db", "error", err)
	}
}

func (s *sessionStore) create(identity string, user User) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.Wrap(err, "failed to generate session token")
	}
	token := hex.EncodeToString(bytes)
	sess := &session{
		token:    token,
		identity: identity,
		userID:   user.ID,
		// Name, not the raw field: the ROOT User is root until they say
		// otherwise, and every surface should get the same answer.
		displayName: user.Name(),
		namespace:   user.Namespace,
		expiresAt:   time.Now().Add(s.expiry),
	}
	s.sessions.Store(token, sess)
	if s.db != nil {
		// A session the db did not take still works until the next restart.
		if _, err := s.db.Exec(`INSERT INTO auth_sessions (hash, identity, user_id, display_name, namespace, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
			hashOf(token), sess.identity, sess.userID, sess.displayName, sess.namespace, sess.expiresAt.Unix()); err != nil {
			s.logger.Warnw("a session was not kept in the db, so a restart ends it", "user", user.ID, "error", err)
		}
	}
	return token, nil
}

// userOf returns who this session is and where they came in, which is not the
// same question as what admitted it. Empty when the deployment keeps no Users.
func (s *sessionStore) userOf(token string) (userID, displayName, namespace string) {
	// Anything else in the map is a wiring mistake, and naming nobody is a
	// better answer to it than panicking inside a request.
	sess, ok := s.load(token)
	if !ok {
		return "", "", ""
	}
	return sess.userID, sess.displayName, sess.namespace
}

func (s *sessionStore) validate(token string) bool {
	_, ok := s.identityOf(token)
	return ok
}

// identityOf returns what admitted this session. The bool is validity, so an
// unnamed identity and an expired session are different answers.
func (s *sessionStore) identityOf(token string) (string, bool) {
	// The map holds only what admit stores; an entry that is not a session
	// is not one to honour.
	sess, ok := s.load(token)
	if !ok {
		s.sessions.Delete(token)
		return "", false
	}
	if time.Now().After(sess.expiresAt) {
		s.sessions.Delete(token)
		s.forget(token)
		return "", false
	}
	return sess.identity, true
}

func (s *sessionStore) invalidate(token string) {
	s.sessions.Delete(token)
	s.forget(token)
}

// endUser ends every session of one User, wherever it was made.
func (s *sessionStore) endUser(userID string) {
	s.sessions.Range(func(key, value any) bool {
		if sess, isSession := value.(*session); isSession && sess.userID == userID {
			s.sessions.Delete(key)
		}
		return true
	})
	if s.db == nil {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM auth_sessions WHERE user_id = ?`, userID); err != nil {
		s.logger.Warnw("a User's sessions were not removed from the db", "user", userID, "error", err)
	}
}

// liveHash reports whether the session a hash names is still live.
func (s *sessionStore) liveHash(hash string) bool {
	now := time.Now()
	live := false
	s.sessions.Range(func(key, value any) bool {
		token, isToken := key.(string)
		sess, isSession := value.(*session)
		if isToken && isSession && hashOf(token) == hash {
			live = !now.After(sess.expiresAt)
			return false
		}
		return true
	})
	if live || s.db == nil {
		return live
	}
	var expires int64
	err := s.db.QueryRow(`SELECT expires_at FROM auth_sessions WHERE hash = ?`, hash).Scan(&expires)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// Not knowing is not ended: a becoming is not undone on a read that failed.
			s.logger.Warnw("whether a session is live was not read from the db", "error", err)
			return true
		}
		return false
	}
	return !now.After(time.Unix(expires, 0))
}

func (s *sessionStore) sweep() {
	now := time.Now()
	s.sessions.Range(func(key, value any) bool {
		sess, isSession := value.(*session)
		if !isSession || now.After(sess.expiresAt) {
			s.sessions.Delete(key)
		}
		return true
	})
	if s.db == nil {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM auth_sessions WHERE expires_at < ?`, now.Unix()); err != nil {
		s.logger.Warnw("expired sessions were not removed from the db", "error", err)
	}
}
