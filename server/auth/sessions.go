package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/teranos/errors"
)

type session struct {
	token string
	// What admitted this session: an entry from auth.root_identities. Empty
	// when nothing in this deployment names an identity, which is the state a
	// passkey-only install stays in.
	identity  string
	expiresAt time.Time
}

type sessionStore struct {
	sessions sync.Map
	expiry   time.Duration
}

func newSessionStore(expiryHours int) *sessionStore {
	return &sessionStore{
		expiry: time.Duration(expiryHours) * time.Hour,
	}
}

func (s *sessionStore) create(identity string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.Wrap(err, "failed to generate session token")
	}
	token := hex.EncodeToString(bytes)
	s.sessions.Store(token, &session{
		token:     token,
		identity:  identity,
		expiresAt: time.Now().Add(s.expiry),
	})
	return token, nil
}

func (s *sessionStore) validate(token string) bool {
	_, ok := s.identityOf(token)
	return ok
}

// identityOf returns what admitted this session. The bool is validity, so an
// unnamed identity and an expired session are different answers.
func (s *sessionStore) identityOf(token string) (string, bool) {
	val, ok := s.sessions.Load(token)
	if !ok {
		return "", false
	}
	sess := val.(*session)
	if time.Now().After(sess.expiresAt) {
		s.sessions.Delete(token)
		return "", false
	}
	return sess.identity, true
}

func (s *sessionStore) invalidate(token string) {
	s.sessions.Delete(token)
}

func (s *sessionStore) sweep() {
	now := time.Now()
	s.sessions.Range(func(key, value interface{}) bool {
		sess := value.(*session)
		if now.After(sess.expiresAt) {
			s.sessions.Delete(key)
		}
		return true
	})
}
